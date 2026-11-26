package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Use the same live activity and source predicates for the representative and
// all results. Moderation and source changes take effect even while queued.
const routeAnchorData = `
	SELECT rr.id, a.user_id, t.activity_id, t.kind,
		CASE WHEN a.distance_m > 0 AND a.distance_m < 'Infinity'::float8 THEN a.distance_m ELSE t.length_m END AS distance_m,
		CASE WHEN a.elevation_gain_m >= 0 AND a.elevation_gain_m < 'Infinity'::float8 THEN a.elevation_gain_m END AS elevation_gain_m,
		CASE WHEN a.start_lat IS NOT NULL AND a.start_lng IS NOT NULL
			AND NOT EXISTS (
				SELECT 1 FROM privacy_zones zone
				WHERE zone.user_id = a.user_id AND ST_DWithin(
					ST_SetSRID(ST_MakePoint(zone.lng, zone.lat), 4326)::geography,
					ST_SetSRID(ST_MakePoint(a.start_lng, a.start_lat), 4326)::geography, zone.radius_m, false)
			) THEN NULLIF(btrim(a.start_place), '') END AS place
	FROM recorded_routes rr
	JOIN recorded_route_tracks t ON t.activity_id = rr.anchor_id
	JOIN activities a ON a.id = t.activity_id ` + currentRouteSource + `
	WHERE ` + currentRouteActivity + ` AND ` + currentRouteTrack + ` AND ` + sharedRouteActivity

const routeRideData = `
	SELECT m.route_id, a.id, a.user_id, a.title, a.start_time, a.timezone,
		CASE WHEN a.distance_m > 0 AND a.distance_m < 'Infinity'::float8 THEN a.distance_m END AS distance_m,
		CASE WHEN a.distance_m > 0 AND a.distance_m < 'Infinity'::float8 AND a.moving_duration_s > 0
			THEN a.distance_m / a.moving_duration_s * 3.6 END AS speed_kmh
	FROM recorded_route_matches m
	JOIN recorded_route_tracks t ON t.activity_id = m.activity_id
	JOIN activities a ON a.id = m.activity_id ` + currentRouteSource + `
	WHERE ` + currentRouteActivity + ` AND ` + currentRouteTrack + ` AND ` + sharedRouteActivity

const routeRiderData = `
	SELECT rides.route_id, u.id, u.username::text AS username, u.display_name,
		COALESCE(u.avatar_url, '') AS avatar_url, count(*) AS rides,
		max(rides.speed_kmh) AS best_speed_kmh, min(rides.start_time) AS first_ride, max(rides.start_time) AS last_ride
	FROM rides JOIN users u ON u.id = rides.user_id
	GROUP BY rides.route_id, u.id
`

// More rides wins; ties use the earliest qualifying ride, then the rider ID.
// Use this order for the featured rider regardless of display sorting.
const routeRiderOrder = `riders.rides DESC, riders.first_ride, riders.id`

func (r *RecordedRouteRepo) List(ctx context.Context, viewer domain.UserID, q port.RecordedRouteQuery) (port.RecordedRouteListing, error) {
	return r.summaries(ctx, viewer, domain.RecordedRouteID{}, q)
}

const routeSummariesSQL = `
		WITH anchors AS (` + routeAnchorData + ` AND ($9::uuid IS NULL OR rr.id = $9)
		), named AS (
			SELECT anchors.*, COALESCE(place, 'Cycling') ||
				CASE kind WHEN 'loop' THEN ' Loop' WHEN 'out_and_back' THEN ' Out & Back' ELSE ' Ride' END ||
				CASE WHEN place IS NULL THEN ' · ' || round(distance_m / 1000)::text || ' km' ELSE '' END AS name
			FROM anchors
			WHERE ($4 = '' OR kind = $4) AND ($5::float8 = 0 OR distance_m >= $5)
				AND ($6::float8 = 0 OR distance_m < $6)
		), rides AS (
			` + routeRideData + ` AND EXISTS (SELECT 1 FROM named WHERE named.id = m.route_id)
		), riders AS (` + routeRiderData + `
		), grouped AS (
			SELECT route_id, sum(rides)::bigint AS total_rides, count(*) AS rider_count,
				max(best_speed_kmh) AS best_speed, max(last_ride) AS last_ride,
				COALESCE(max(rides) FILTER (WHERE lower(username) = lower($2)), 0) AS selected_rides
			FROM riders GROUP BY route_id
			HAVING max(rides) >= 2 AND ($2 = '' OR bool_or(lower(username) = lower($2)))
				AND (NOT $7::boolean OR NOT bool_or(id = $1))
		), matching AS MATERIALIZED (
			SELECT named.*, grouped.total_rides, grouped.rider_count, grouped.best_speed, grouped.last_ride, grouped.selected_rides
			FROM named JOIN grouped ON grouped.route_id = named.id
			WHERE $3 = '' OR strpos(lower(named.name), lower($3)) > 0
		), page AS (
			SELECT matching.*, row_number() OVER (ORDER BY
				CASE WHEN $8 = 'rider_rides' THEN selected_rides END DESC,
				CASE WHEN $8 = 'riders' THEN rider_count END DESC,
				CASE WHEN $8 = 'recent' THEN last_ride END DESC,
				CASE WHEN $8 = 'distance' THEN distance_m END DESC,
				CASE WHEN $8 = 'elevation' THEN elevation_gain_m END DESC NULLS LAST,
				CASE WHEN $8 = 'speed' THEN best_speed END DESC NULLS LAST,
				total_rides DESC, id) AS position
			FROM matching ORDER BY position LIMIT $10 OFFSET $11
		)
		SELECT (SELECT count(*) FROM matching), page.id, page.user_id, page.name, page.kind, page.distance_m,
			page.elevation_gain_m, page.total_rides, page.rider_count, page.best_speed,
			CASE WHEN $9::uuid IS NULL THEN t.preview ELSE t.track END, rider_cards.cards
		FROM (SELECT 1) base
		LEFT JOIN page ON true
		LEFT JOIN recorded_route_tracks t ON t.activity_id = page.activity_id
		LEFT JOIN LATERAL (
			SELECT jsonb_agg(to_jsonb(card) ORDER BY card.rides DESC, card.first_ride, card.id) AS cards
			FROM (
				SELECT id, username, display_name, avatar_url, rides, best_speed_kmh, first_ride
				FROM riders WHERE route_id = page.id
				ORDER BY ` + routeRiderOrder + ` LIMIT CASE WHEN $9::uuid IS NULL THEN 3 ELSE NULL END
			) card
		) rider_cards ON page.id IS NOT NULL
		ORDER BY page.position`

func (r *RecordedRouteRepo) summaries(ctx context.Context, viewer domain.UserID, id domain.RecordedRouteID, q port.RecordedRouteQuery) (port.RecordedRouteListing, error) {
	// Only the selected page loads geometry. Route statistics use indexed
	// membership and current activity summaries, without reading raw GPS.
	rows, err := dbtx(ctx, r.pool).Query(ctx, routeSummariesSQL, viewer.UUID(), q.Rider, q.Search, string(q.Kind), q.MinDistanceM, q.MaxDistanceM,
		q.Unridden, q.Sort, nullableRecordedRoute(id), q.Limit, q.Offset)
	if err != nil {
		return port.RecordedRouteListing{}, fmt.Errorf("query recorded routes: %w", err)
	}
	defer rows.Close()
	out := port.RecordedRouteListing{}
	for rows.Next() {
		var route port.RecordedRouteSummary
		var routeID *domain.RecordedRouteID
		var owner *domain.UserID
		var name *string
		var kind *domain.RecordedRouteKind
		var distance *float64
		var rides, riderCount *int
		var track trackJSON
		var people []routeRiderJSON
		if err := rows.Scan(&out.Total, &routeID, &owner, &name, &kind, &distance, &route.ElevationM, &rides,
			&riderCount, &route.BestSpeedKmh, &track, &people); err != nil {
			return out, fmt.Errorf("scan recorded route: %w", err)
		}
		if routeID == nil {
			continue
		}
		route.ID, route.Owner, route.Name, route.Kind = *routeID, *owner, *name, *kind
		route.DistanceM, route.Rides, route.RiderCount = *distance, *rides, *riderCount
		route.Track = track.track()
		for _, person := range people {
			route.Riders = append(route.Riders, port.RecordedRouteRider{UserID: domain.UserID(person.UserID),
				Username: person.Username, DisplayName: person.DisplayName, AvatarURL: person.AvatarURL,
				Rides: person.Rides, BestSpeedKmh: person.BestSpeedKmh})
		}
		out.Routes = append(out.Routes, route)
	}
	return out, rows.Err()
}

type routeRiderJSON struct {
	UserID       uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name"`
	AvatarURL    string    `json:"avatar_url"`
	Rides        int       `json:"rides"`
	BestSpeedKmh *float64  `json:"best_speed_kmh"`
}

func nullableRecordedRoute(id domain.RecordedRouteID) any {
	if id == (domain.RecordedRouteID{}) {
		return nil
	}
	return id.UUID()
}

func (r *RecordedRouteRepo) Detail(ctx context.Context, viewer domain.UserID, id domain.RecordedRouteID, offset, limit int) (port.RecordedRouteDetail, error) {
	// The detail and its recent rides use one snapshot.
	var out port.RecordedRouteDetail
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ctx = withTx(ctx, tx)
	found, err := r.summaries(ctx, viewer, id, port.RecordedRouteQuery{Limit: 1})
	if err != nil {
		return out, err
	}
	if len(found.Routes) == 0 {
		return out, domain.ErrNotFound
	}
	out.RecordedRouteSummary = found.Routes[0]
	if limit > 0 {
		out.Recent, err = r.routeRecent(ctx, id, offset, limit)
		if err != nil {
			return out, err
		}
	}
	return out, tx.Commit(ctx)
}

func (r *RecordedRouteRepo) routeRecent(ctx context.Context, id domain.RecordedRouteID, offset, limit int) ([]port.RecordedRouteRide, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, routeRideData+` AND m.route_id = $1
		ORDER BY a.start_time DESC, a.id DESC LIMIT $2 OFFSET $3`, id.UUID(), limit, offset)
	if err != nil {
		return nil, err
	}
	return pgCollectRows(rows, func(row rowScanner) (port.RecordedRouteRide, error) {
		var out port.RecordedRouteRide
		var route domain.RecordedRouteID
		err := row.Scan(&route, &out.ID, &out.Rider, &out.Title, &out.StartTime, &out.Timezone, &out.DistanceM, &out.SpeedKmh)
		return out, err
	}, "scan route ride", "iterate route rides")
}

var _ port.RecordedRouteReader = (*RecordedRouteRepo)(nil)
