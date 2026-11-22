package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Apply the stored activity audience and category policies before ranking.
// The default policy comes from domain, so missing audience keys fall back
// in the same order as ResolveVisibility. Comparisons do not use share links.
func comparisonRidesSQL(ctx context.Context) string {
	return `
		WITH audience AS (
			SELECT CASE WHEN EXISTS (
				SELECT 1 FROM follows
				WHERE follower_id = $1 AND followee_id = $2 AND status = 'accepted'
			) THEN 'followers' ELSE 'public' END AS name
		), rides AS (
			SELECT a.id, a.user_id, a.primary_stream_source_id,
				COALESCE(a.visibility_override -> audience.name,
				         defaults.policy -> audience.name, $3::jsonb -> audience.name) AS categories
			FROM activities a
			CROSS JOIN audience
			LEFT JOIN user_visibility_defaults defaults ON defaults.user_id = a.user_id
			WHERE a.user_id IN ($1, $2) AND a.type = 'ride' AND a.deleted_at IS NULL
				AND (a.user_id = $1 OR (
					(a.privacy = 'public' OR (a.privacy = 'followers' AND audience.name = 'followers'))
					` + communityReadSQL(port.WithActivityReadViewer(ctx, false), "a.") + `
				))
		)`
}

// Head-to-Head uses recorded local efforts. Derived sources may copy data
// from another ride whose permissions cannot be inferred from this one.
const comparisonSourceSQL = `src.status <> 'detached' AND src.provider <> 'derived'
	AND (src.provider <> 'strava' OR a.user_id = $1)`

func (r *SegmentRepo) ListSharedSegments(ctx context.Context, viewer, opponent domain.UserID) ([]domain.SharedSegment, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, comparisonRidesSQL(ctx)+`,
		recordings AS (
			SELECT e.id, e.segment_id, e.user_id, e.activity_id, e.elapsed_s, e.start_time,
				dense_rank() OVER (PARTITION BY e.segment_id, e.activity_id ORDER BY
					(e.activity_source_id = a.primary_stream_source_id) DESC NULLS LAST, e.activity_source_id) AS source_order
			FROM rides a
			JOIN segment_efforts e ON e.activity_id = a.id AND e.user_id = a.user_id
			JOIN activity_sources src ON src.id = e.activity_source_id AND src.activity_id = a.id
			JOIN segments s ON s.id = e.segment_id
			WHERE e.user_id IN ($1, $2)
				AND s.activity_type = 'ride' AND s.distance_m > 0 AND s.distance_m < 'Infinity'::float8
				AND e.elapsed_s > 0 AND e.elapsed_s < 'Infinity'::float8
				AND (e.provider_effort_external_id IS NULL OR (
					s.source <> 'strava_catalog' AND src.start_lat IS NOT NULL AND src.parsed @> '{"has_stream":true}'
				))
				AND `+comparisonSourceSQL+`
				AND (a.user_id = $1 OR a.categories ? 'segments')
				AND (s.source = 'strava_catalog'
					OR (s.source = 'native' AND (s.scope = 'instance' OR s.owner_user_id = $1))
					OR (s.source = 'external' AND EXISTS (
						SELECT 1 FROM external_accounts WHERE id = s.external_account_id AND user_id = $1
					)))
		), best AS (
			SELECT DISTINCT ON (segment_id, user_id) segment_id, user_id, activity_id, elapsed_s
			FROM recordings
			WHERE source_order = 1
			ORDER BY segment_id, user_id, elapsed_s, start_time, id
		)
		SELECT s.id, s.name, s.distance_m, you.activity_id, you.elapsed_s, them.activity_id, them.elapsed_s
		FROM best you
		JOIN best them ON them.segment_id = you.segment_id AND them.user_id = $2
		JOIN segments s ON s.id = you.segment_id
		WHERE you.user_id = $1
		ORDER BY lower(s.name), s.id`,
		viewer.UUID(), opponent.UUID(), domain.MarshalPolicy(domain.DefaultVisibilityPolicy()))
	if err != nil {
		return nil, fmt.Errorf("list shared segments: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (domain.SharedSegment, error) {
		var s domain.SharedSegment
		var id, you, them uuid.UUID
		err := row.Scan(&id, &s.Name, &s.Distance, &you, &s.You.Value, &them, &s.Opponent.Value)
		s.ID, s.You.ActivityID, s.Opponent.ActivityID = domain.SegmentID(id), domain.ActivityID(you), domain.ActivityID(them)
		return s, err
	}, "scan shared segment", "iterate shared segments")
}

func (r *BestEffortRepo) ListComparisonRecords(ctx context.Context, viewer, opponent domain.UserID) ([]domain.MemberRecord, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, comparisonRidesSQL(ctx)+`
		SELECT DISTINCT ON (b.user_id, b.metric, b.window_kind, b.window_value)
			b.user_id, b.metric, b.window_kind, b.window_value, b.activity_id, b.achieved_value
		FROM rides a
		JOIN best_efforts b ON b.activity_id = a.id AND b.user_id = a.user_id
		JOIN activity_sources src ON src.id = b.activity_source_id AND src.activity_id = a.id
		WHERE b.user_id IN ($1, $2) AND b.activity_type = 'ride'
			AND `+comparisonSourceSQL+`
			AND `+validRecordSQL+`
			AND ((b.metric = 'pace' AND b.window_kind = 'distance')
				OR (b.metric <> 'pace' AND b.window_kind = 'duration'
					-- Existing duration efforts use N samples. At 1 Hz their endpoints span N-1 seconds.
					AND b.duration_s BETWEEN b.window_value - 1.5 AND b.window_value + 0.5))
			AND (a.user_id = $1 OR (a.categories ? 'best_efforts'
				AND CASE b.metric
					WHEN 'power' THEN a.categories ? 'power'
					WHEN 'heart_rate' THEN a.categories ? 'hr'
					WHEN 'speed' THEN a.categories ? 'pace_speed'
					WHEN 'pace' THEN a.categories ? 'pace_speed'
					ELSE true
				END))
		ORDER BY b.user_id, b.metric, b.window_kind, b.window_value,
			CASE WHEN b.metric = ANY($4) THEN b.achieved_value ELSE -b.achieved_value END, b.ts, b.id`,
		viewer.UUID(), opponent.UUID(), domain.MarshalPolicy(domain.DefaultVisibilityPolicy()), smallerIsBetterMetrics())
	if err != nil {
		return nil, fmt.Errorf("list comparison records: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (domain.MemberRecord, error) {
		var rec domain.MemberRecord
		var user, activity uuid.UUID
		var metric, kind string
		err := row.Scan(&user, &metric, &kind, &rec.Bucket.WindowValue, &activity, &rec.Effort.Value)
		rec.UserID, rec.Effort.ActivityID = domain.UserID(user), domain.ActivityID(activity)
		rec.Bucket.ActivityType = domain.ActivityTypeRide
		rec.Bucket.Metric, rec.Bucket.WindowKind = domain.BestEffortMetric(metric), domain.BestEffortWindowKind(kind)
		return rec, err
	}, "scan comparison record", "iterate comparison records")
}
