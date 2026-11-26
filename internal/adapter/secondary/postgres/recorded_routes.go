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

func (r *RecordedRouteRepo) EnqueueRecluster(ctx context.Context) (bool, error) {
	// Consume the upgrade marker and enqueue the history in one transaction.
	// The upgraded worker owns this step, even if migrations ran separately.
	var queued bool
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		WITH upgrade AS (DELETE FROM recorded_route_recluster RETURNING true),
		queued AS (
			INSERT INTO recorded_route_queue (activity_id, available_at, live)
			SELECT a.id, LEAST(a.start_time, now()), false FROM activities a
			WHERE EXISTS (SELECT 1 FROM upgrade) AND (
				(a.type = 'ride' AND a.deleted_at IS NULL AND a.primary_stream_source_id IS NOT NULL)
				OR EXISTS (SELECT 1 FROM recorded_route_tracks t WHERE t.activity_id = a.id))
			ORDER BY a.id
			ON CONFLICT (activity_id) DO NOTHING
		) SELECT EXISTS (SELECT 1 FROM upgrade)`).Scan(&queued)
	return queued, err
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
		SELECT src.id, src.heatmap_revision, (`+sharedRouteActivity+`)
		FROM activities a `+currentRouteSource+`
		WHERE a.id = $1 AND `+currentRouteActivity, id.UUID()).Scan(&out.Source, &out.Revision, &out.Shared)
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

func (r *RecordedRouteRepo) Membership(ctx context.Context, id domain.ActivityID) (domain.RecordedRouteID, error) {
	var out domain.RecordedRouteID
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT id FROM recorded_routes WHERE anchor_id = $1
		UNION ALL
		SELECT route_id FROM recorded_route_matches WHERE activity_id = $1
		LIMIT 1`, id.UUID()).Scan(&out)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return out, err
}

const routeCandidateData = `
	SELECT t.activity_id, m.route_id, a.user_id, a.start_time, t.track
	FROM recorded_route_tracks t
	JOIN activities a ON a.id = t.activity_id ` + currentRouteSource + `
	LEFT JOIN recorded_route_matches m ON m.activity_id = t.activity_id
	WHERE ` + currentRouteActivity + ` AND ` + currentRouteTrack + ` AND ` + sharedRouteActivity

func (r *RecordedRouteRepo) Candidate(ctx context.Context, id domain.ActivityID) (port.RouteCandidate, error) {
	out, err := scanRouteCandidate(dbtx(ctx, r.pool).QueryRow(ctx, routeCandidateData+` AND t.activity_id = $1`, id.UUID()))
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return out, err
}

func (r *RecordedRouteRepo) Nearby(ctx context.Context, refs, exclude []domain.ActivityID) ([]port.RouteCandidate, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		WITH nearby AS MATERIALIZED (
			SELECT DISTINCT t.activity_id
			FROM recorded_route_tracks ref
			CROSS JOIN LATERAL (
				SELECT ST_Expand(ref.bounds, 0.0004 / greatest(0.000003,
					cos(radians(greatest(abs(ST_YMin(ref.bounds)), abs(ST_YMax(ref.bounds)))))), 0.0004) AS bounds
			) area
			JOIN recorded_route_tracks t ON t.length_m BETWEEN ref.length_m * $3 AND ref.length_m / $3
				AND (t.bounds && area.bounds
					OR (ST_XMin(area.bounds) < -180 AND t.bounds && ST_Translate(area.bounds, 360, 0))
					OR (ST_XMax(area.bounds) > 180 AND t.bounds && ST_Translate(area.bounds, -360, 0)))
			WHERE ref.activity_id = ANY($1) AND t.activity_id <> ALL($2)
		)
		`+routeCandidateData+` AND t.activity_id IN (SELECT activity_id FROM nearby)
		ORDER BY a.start_time, a.id`, activityUUIDs(refs), activityUUIDs(exclude), domain.MinRecordedRouteLengthRatio)
	if err != nil {
		return nil, err
	}
	return pgCollectRows(rows, scanRouteCandidate, "scan route candidate", "iterate route candidates")
}

func (r *RecordedRouteRepo) Members(ctx context.Context, id domain.RecordedRouteID) ([]port.RouteCandidate, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, routeCandidateData+`
		AND m.route_id = $1 ORDER BY a.start_time, a.id`, id.UUID())
	if err != nil {
		return nil, err
	}
	return pgCollectRows(rows, scanRouteCandidate, "scan route member", "iterate route members")
}

func scanRouteCandidate(row rowScanner) (port.RouteCandidate, error) {
	var out port.RouteCandidate
	var routeID *domain.RecordedRouteID
	var raw trackJSON
	err := row.Scan(&out.Activity, &routeID, &out.Rider, &out.StartTime, &raw)
	if routeID != nil {
		out.Route = *routeID
	}
	out.Track = raw.track()
	return out, err
}

func (r *RecordedRouteRepo) Orphan(ctx context.Context) (domain.RecordedRouteID, error) {
	var route domain.RecordedRouteID
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT id FROM recorded_routes rr WHERE anchor_id IS NULL AND (
			NOT EXISTS (SELECT 1 FROM recorded_route_matches WHERE route_id = rr.id)
			OR EXISTS (`+routeCandidateData+` AND m.route_id = rr.id)
		) ORDER BY id LIMIT 1`).Scan(&route)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return route, err
}

func (r *RecordedRouteRepo) SaveCluster(ctx context.Context, routes []domain.RecordedRouteID, representative domain.ActivityID, matches []port.RouteMatch) error {
	tx := dbtx(ctx, r.pool)
	ids := make([]string, len(routes))
	for i, route := range routes {
		ids[i] = route.String()
	}
	if len(matches) != 0 {
		var route domain.RecordedRouteID
		if len(routes) == 0 {
			if err := tx.QueryRow(ctx, `INSERT INTO recorded_routes DEFAULT VALUES RETURNING id`).Scan(&route); err != nil {
				return err
			}
		} else {
			route = routes[0]
			ids = ids[1:]
		}
		// The representative may have been the anchor of a route being merged.
		if _, err := tx.Exec(ctx, `UPDATE recorded_routes SET anchor_id = NULL WHERE id = ANY($1) AND anchor_id = $2`,
			ids, representative.UUID()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE recorded_routes SET anchor_id = $2 WHERE id = $1 AND anchor_id IS DISTINCT FROM $2`,
			route.UUID(), representative.UUID()); err != nil {
			return err
		}
		if err := r.saveRouteMatches(ctx, route, matches); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `
		WITH removed AS (
			DELETE FROM recorded_route_matches WHERE route_id = ANY($1) RETURNING activity_id
		)
		INSERT INTO recorded_route_queue (activity_id, available_at)
		SELECT activity_id, now() FROM removed ON CONFLICT DO NOTHING`, ids)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM recorded_routes WHERE id = ANY($1)`, ids)
	return err
}

func (r *RecordedRouteRepo) saveRouteMatches(ctx context.Context, route domain.RecordedRouteID, matches []port.RouteMatch) error {
	ids := make([]string, len(matches))
	scores := make([]float64, len(matches))
	for i, match := range matches {
		ids[i], scores[i] = match.Activity.String(), match.Overlap
	}
	tx := dbtx(ctx, r.pool)
	if _, err := tx.Exec(ctx, `
		INSERT INTO recorded_route_matches (activity_id, route_id, overlap)
		SELECT activity_id, $1, score FROM unnest($2::uuid[], $3::float8[]) AS matches(activity_id, score)
		ON CONFLICT (activity_id) DO UPDATE SET route_id = EXCLUDED.route_id, overlap = EXCLUDED.overlap
		WHERE (recorded_route_matches.route_id, recorded_route_matches.overlap) IS DISTINCT FROM (EXCLUDED.route_id, EXCLUDED.overlap)`,
		route.UUID(), ids, scores); err != nil {
		return err
	}
	// All these tracks have been reclustered. Keep live jobs and their
	// generation checks, since a correction could have arrived in parallel.
	if _, err := tx.Exec(ctx, `DELETE FROM recorded_route_queue WHERE NOT live AND activity_id = ANY($1)`, ids); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		WITH removed AS (
			DELETE FROM recorded_route_matches WHERE route_id = $1 AND activity_id <> ALL($2) RETURNING activity_id
		)
		INSERT INTO recorded_route_queue (activity_id, available_at)
		SELECT activity_id, now() FROM removed ON CONFLICT DO NOTHING`, route.UUID(), ids)
	return err
}

func activityUUIDs(ids []domain.ActivityID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

var _ port.RecordedRouteWriter = (*RecordedRouteRepo)(nil)
