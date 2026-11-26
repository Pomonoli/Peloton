package postgres

import (
	"context"
	"errors"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type RecordedRouteRepo struct {
	pool *pgxpool.Pool
}

func NewRecordedRouteRepo(pool *pgxpool.Pool) *RecordedRouteRepo {
	return &RecordedRouteRepo{pool: pool}
}

func (r *RecordedRouteRepo) Lock(ctx context.Context) (bool, error) {
	var locked bool
	err := dbtx(ctx, r.pool).QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(721589, 90)`).Scan(&locked)
	return locked, err
}

func (r *RecordedRouteRepo) Next(ctx context.Context) (port.RouteJob, error) {
	var out port.RouteJob
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT activity_id, generation FROM recorded_route_queue
		WHERE available_at <= now() ORDER BY live DESC, available_at, activity_id LIMIT 1`).Scan(&out.Activity, &out.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return out, err
}

func (r *RecordedRouteRepo) Finish(ctx context.Context, job port.RouteJob) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `
		DELETE FROM recorded_route_queue WHERE activity_id = $1 AND generation = $2`, job.Activity.UUID(), job.Generation)
	return err
}

func (r *RecordedRouteRepo) Retry(ctx context.Context, job port.RouteJob) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `
		UPDATE recorded_route_queue SET attempts = attempts + 1,
		    available_at = now() + make_interval(secs => 5 * (1 << LEAST(attempts, 9)))
		WHERE activity_id = $1 AND generation = $2`, job.Activity.UUID(), job.Generation)
	return err
}

const currentRouteSource = `
	JOIN activity_sources src ON src.id = a.primary_stream_source_id AND src.activity_id = a.id
		AND src.status <> 'detached'
	JOIN users u ON u.id = a.user_id AND u.status <> 'deleted'`
const currentRouteActivity = `a.type = 'ride' AND a.deleted_at IS NULL`
const currentRouteTrack = `t.source_id = src.id AND t.source_revision = src.heatmap_revision`
const sharedRouteActivity = `NOT a.hidden_by_admin AND NOT a.provider_restricted`

func (r *RecordedRouteRepo) Input(ctx context.Context, id domain.ActivityID) (port.RouteInput, error) {
	var out port.RouteInput
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT src.id, src.heatmap_revision, a.user_id, (`+sharedRouteActivity+`)
		FROM activities a `+currentRouteSource+`
		WHERE a.id = $1 AND `+currentRouteActivity, id.UUID()).Scan(&out.Source, &out.Revision, &out.Rider, &out.Shared)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return out, err
}

type routePointJSON struct {
	Lat        float64  `json:"y"`
	Lon        float64  `json:"x"`
	ElevationM *float64 `json:"e,omitempty"`
	DistanceM  float64  `json:"d,omitempty"`
}

type trackJSON [][]routePointJSON

func encodeTrack(track domain.RecordedTrack) trackJSON {
	out := make(trackJSON, len(track))
	for i, line := range track {
		out[i] = make([]routePointJSON, len(line))
		for j, p := range line {
			out[i][j] = routePointJSON(p)
		}
	}
	return out
}

func (encoded trackJSON) track() domain.RecordedTrack {
	out := make(domain.RecordedTrack, len(encoded))
	for i, line := range encoded {
		out[i] = make([]domain.RecordedRoutePoint, len(line))
		for j, p := range line {
			out[i][j] = domain.RecordedRoutePoint(p)
		}
	}
	return out
}

func (r *RecordedRouteRepo) Track(ctx context.Context, id domain.ActivityID) (port.RouteTrack, error) {
	out := port.RouteTrack{Activity: id}
	var raw trackJSON
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT source_id, source_revision, length_m, kind, track
		FROM recorded_route_tracks WHERE activity_id = $1`, id.UUID()).
		Scan(&out.Source, &out.Revision, &out.LengthM, &out.Kind, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	out.Track = raw.track()
	return out, err
}

func (r *RecordedRouteRepo) SaveTrack(ctx context.Context, t port.RouteTrack) error {
	west, south, east, north := 180.0, 90.0, -180.0, -90.0
	for _, line := range t.Track {
		for _, p := range line {
			west, south = math.Min(west, p.Lon), math.Min(south, p.Lat)
			east, north = math.Max(east, p.Lon), math.Max(north, p.Lat)
		}
	}
	_, err := dbtx(ctx, r.pool).Exec(ctx, `
		INSERT INTO recorded_route_tracks (activity_id, source_id, source_revision, length_m, kind, bounds, track, preview)
		VALUES ($1, $2, $3, $4, $5, ST_MakeEnvelope($6, $7, $8, $9, 4326), $10, $11)
		ON CONFLICT (activity_id) DO UPDATE SET source_id = EXCLUDED.source_id, source_revision = EXCLUDED.source_revision,
			length_m = EXCLUDED.length_m, kind = EXCLUDED.kind, bounds = EXCLUDED.bounds,
			track = EXCLUDED.track, preview = EXCLUDED.preview`, t.Activity.UUID(), t.Source.UUID(), t.Revision,
		t.LengthM, t.Kind, west, south, east, north, encodeTrack(t.Track), encodeTrack(t.Track.Preview(120)))
	return err
}

func (r *RecordedRouteRepo) DeleteTrack(ctx context.Context, id domain.ActivityID) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `DELETE FROM recorded_route_tracks WHERE activity_id = $1`, id.UUID())
	return err
}

func (r *RecordedRouteRepo) Membership(ctx context.Context, id domain.ActivityID) (port.RouteMembership, error) {
	var out port.RouteMembership
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT id, true FROM recorded_routes WHERE anchor_id = $1
		UNION ALL
		SELECT route_id, false FROM recorded_route_matches WHERE activity_id = $1
		ORDER BY 2 DESC LIMIT 1`, id.UUID()).Scan(&out.Route, &out.Anchor)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return out, err
}

func (r *RecordedRouteRepo) Candidates(ctx context.Context, ref domain.ActivityID, q port.RouteCandidates) ([]port.RouteCandidate, error) {
	join := `LEFT JOIN recorded_route_matches m ON m.activity_id = t.activity_id`
	filter := ""
	routeID := "m.route_id"
	args := []any{ref.UUID(), domain.MinRecordedRouteOverlap}
	switch {
	case q.Anchors:
		join = `JOIN recorded_routes rr ON rr.anchor_id = t.activity_id`
		routeID = "rr.id"
	case q.Unassigned:
		filter = " AND m.activity_id IS NULL"
	default:
		args = append(args, q.MembersOf.UUID())
		filter = " AND m.route_id = $3"
	}
	filter += " AND " + sharedRouteActivity
	if q.Rider != (domain.UserID{}) {
		// Rider filtering is only used when looking for an unassigned second ride.
		args = append(args, q.Rider.UUID())
		filter += " AND a.user_id = $3"
	}
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		SELECT t.activity_id, `+routeID+`, t.kind, t.track
		FROM recorded_route_tracks ref
		CROSS JOIN LATERAL (
			SELECT ST_Expand(ref.bounds, 0.0004 / greatest(0.000003,
				cos(radians(greatest(abs(ST_YMin(ref.bounds)), abs(ST_YMax(ref.bounds)))))), 0.0004) AS bounds
		) area
		JOIN recorded_route_tracks t ON t.activity_id <> ref.activity_id
			AND t.length_m BETWEEN ref.length_m * $2 AND ref.length_m / $2
			AND (t.bounds && area.bounds
				OR (ST_XMin(area.bounds) < -180 AND t.bounds && ST_Translate(area.bounds, 360, 0))
				OR (ST_XMax(area.bounds) > 180 AND t.bounds && ST_Translate(area.bounds, -360, 0)))
		JOIN activities a ON a.id = t.activity_id `+currentRouteSource+join+`
		WHERE ref.activity_id = $1 AND `+currentRouteActivity+` AND `+currentRouteTrack+filter+`
		ORDER BY a.start_time, a.id`, args...)
	if err != nil {
		return nil, err
	}
	return pgCollectRows(rows, scanRouteCandidate, "scan route candidate", "iterate route candidates")
}

func scanRouteCandidate(row rowScanner) (port.RouteCandidate, error) {
	var out port.RouteCandidate
	var routeID *domain.RecordedRouteID
	var raw trackJSON
	err := row.Scan(&out.Activity, &routeID, &out.Kind, &raw)
	if routeID != nil {
		out.Route = *routeID
	}
	out.Track = raw.track()
	return out, err
}

func (r *RecordedRouteRepo) Orphan(ctx context.Context) (domain.RecordedRouteID, domain.ActivityID, error) {
	var route domain.RecordedRouteID
	var id domain.ActivityID
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT rr.id, m.activity_id
		FROM recorded_routes rr
		JOIN recorded_route_matches m ON m.route_id = rr.id
		JOIN recorded_route_tracks t ON t.activity_id = m.activity_id
		JOIN activities a ON a.id = m.activity_id `+currentRouteSource+`
		WHERE rr.anchor_id IS NULL AND `+currentRouteActivity+` AND `+currentRouteTrack+` AND `+sharedRouteActivity+`
		ORDER BY rr.id, m.overlap DESC, a.start_time, a.id LIMIT 1`).Scan(&route, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return route, id, err
}

func (r *RecordedRouteRepo) Anchor(ctx context.Context, id domain.RecordedRouteID) (port.RouteCandidate, error) {
	out, err := scanRouteCandidate(dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT t.activity_id, rr.id, t.kind, t.track
		FROM recorded_routes rr
		JOIN recorded_route_tracks t ON t.activity_id = rr.anchor_id
		JOIN activities a ON a.id = t.activity_id `+currentRouteSource+`
		WHERE rr.id = $1 AND `+currentRouteActivity+` AND `+currentRouteTrack+` AND `+sharedRouteActivity, id.UUID()))
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return out, err
}

func (r *RecordedRouteRepo) SetAnchor(ctx context.Context, id domain.RecordedRouteID, activity domain.ActivityID) error {
	var anchor any
	if activity != (domain.ActivityID{}) {
		anchor = activity.UUID()
	}
	_, err := dbtx(ctx, r.pool).Exec(ctx, `UPDATE recorded_routes SET anchor_id = $2 WHERE id = $1`,
		id.UUID(), anchor)
	return err
}

func (r *RecordedRouteRepo) Create(ctx context.Context, activity domain.ActivityID) (domain.RecordedRouteID, error) {
	var id domain.RecordedRouteID
	err := dbtx(ctx, r.pool).QueryRow(ctx, `INSERT INTO recorded_routes (anchor_id) VALUES ($1) RETURNING id`,
		activity.UUID()).Scan(&id)
	return id, err
}

func (r *RecordedRouteRepo) SetMatch(ctx context.Context, activity domain.ActivityID, id domain.RecordedRouteID, score float64) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `
		INSERT INTO recorded_route_matches (activity_id, route_id, overlap) VALUES ($1, $2, $3)
		ON CONFLICT (activity_id) DO UPDATE SET route_id = EXCLUDED.route_id, overlap = EXCLUDED.overlap`, activity.UUID(), id.UUID(), score)
	return err
}

func (r *RecordedRouteRepo) RemoveMatch(ctx context.Context, id domain.ActivityID) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `DELETE FROM recorded_route_matches WHERE activity_id = $1`, id.UUID())
	return err
}

func (r *RecordedRouteRepo) RetainMatches(ctx context.Context, id domain.RecordedRouteID, retain []domain.ActivityID) error {
	ids := make([]string, len(retain))
	for i, activity := range retain {
		ids[i] = activity.String()
	}
	_, err := dbtx(ctx, r.pool).Exec(ctx, `
		WITH removed AS (
			DELETE FROM recorded_route_matches WHERE route_id = $1 AND activity_id <> ALL($2::uuid[])
			RETURNING activity_id
		)
		INSERT INTO recorded_route_queue (activity_id, available_at)
		SELECT activity_id, now() FROM removed
		ON CONFLICT (activity_id) DO NOTHING`, id.UUID(), ids)
	return err
}

var _ port.RecordedRouteWriter = (*RecordedRouteRepo)(nil)
