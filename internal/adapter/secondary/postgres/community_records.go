package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// Garmin and Peloton's file parsers store recorded watt channels. Other
// providers must attest provenance. An explicit estimated flag wins over
// the provider default. Peloton's browser estimates never reach this table.
const measuredPowerSQL = "COALESCE((src.parsed->>'power_recorded')::boolean, src.provider IN ('garmin', 'upload'))"

// validRecordSQL keeps community comparisons on plausible, measured efforts.
var validRecordSQL = validCyclingMotionRecordSQL + ` AND b.achieved_value > 0 AND b.achieved_value < 'Infinity'::float8
	AND CASE b.metric
		WHEN 'power' THEN b.achieved_value <= 3000 AND ` + measuredPowerSQL + `
		WHEN 'heart_rate' THEN b.achieved_value BETWEEN 25 AND 250
		WHEN 'speed' THEN b.achieved_value <= 50
		WHEN 'pace' THEN b.achieved_value >= 20
		WHEN 'vam' THEN b.achieved_value <= 15000
		ELSE false
	END`

func (r *BestEffortRepo) ListCommunityRecords(ctx context.Context, bucket *domain.RecordBucket) ([]domain.CommunityRecord, error) {
	args := []any{smallerIsBetterMetrics()}
	filter, winners := "", "WHERE place = 1"
	if bucket != nil {
		filter = " AND b.activity_type = $2 AND b.metric = $3 AND b.window_kind = $4 AND b.window_value = $5"
		args = append(args, string(bucket.ActivityType), string(bucket.Metric), string(bucket.WindowKind), bucket.WindowValue)
		winners = ""
	}
	// Exclude stale source rows and implausible values before choosing each
	// person's best eligible effort.
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		WITH eligible AS (
			SELECT b.id, b.user_id, b.activity_id, b.activity_type, b.metric, b.window_kind, b.window_value,
				b.achieved_value, b.ts, CASE WHEN b.metric = ANY($1) THEN b.achieved_value ELSE -b.achieved_value END AS score
			FROM best_efforts b
			JOIN activities a ON a.id = b.activity_id AND a.type = b.activity_type AND a.user_id = b.user_id
			JOIN activity_sources src ON src.id = b.activity_source_id AND src.activity_id = a.id AND src.status <> 'detached'
			WHERE a.deleted_at IS NULL AND NOT a.hidden_by_admin AND NOT a.provider_restricted
				AND src.provider <> 'strava'`+sharedRecordSQL("a.", communityViewerSQL(ctx))+` AND `+recordVariantSQL(ctx)+` AND `+recordDistanceSQL+` AND `+validRecordSQL+filter+`
		), personal AS (
			SELECT DISTINCT ON (activity_type, metric, window_kind, window_value, user_id) *
			FROM eligible
			ORDER BY activity_type, metric, window_kind, window_value, user_id, score, ts, id
		), ranked AS (
			SELECT p.*, u.username::text, COALESCE(NULLIF(u.display_name, ''), u.username::text) AS display_name,
				rank() OVER (category ORDER BY score) AS rank,
				row_number() OVER (category ORDER BY score, ts, user_id, p.id) AS place,
				count(*) OVER category AS members
			FROM personal p JOIN users u ON u.id = p.user_id AND u.status <> 'deleted'
			WINDOW category AS (PARTITION BY activity_type, metric, window_kind, window_value)
		)
		SELECT activity_type, metric, window_kind, window_value, rank, members,
			user_id, username, display_name, activity_id, achieved_value, ts
		FROM ranked `+winners+`
		ORDER BY activity_type, metric, window_kind, window_value, place`, args...)
	if err != nil {
		return nil, fmt.Errorf("list community records: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (domain.CommunityRecord, error) {
		var result domain.CommunityRecord
		var uid, aid uuid.UUID
		var at, metric, kind string
		err := row.Scan(&at, &metric, &kind, &result.Bucket.WindowValue, &result.Rank, &result.Members,
			&uid, &result.Username, &result.DisplayName, &aid, &result.AchievedValue, &result.Timestamp)
		result.Bucket.ActivityType, result.Bucket.Metric, result.Bucket.WindowKind = domain.ActivityType(at), domain.BestEffortMetric(metric), domain.BestEffortWindowKind(kind)
		result.MemberID, result.ActivityID = domain.UserID(uid), domain.ActivityID(aid)
		return result, err
	}, "scan community record", "iterate community records")
}
