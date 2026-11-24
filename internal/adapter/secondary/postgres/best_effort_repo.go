package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// BestEffortRepo implements port.BestEffortRepo.
type BestEffortRepo struct {
	pool *pgxpool.Pool
}

func (r *BestEffortRepo) LockBestEffortSource(ctx context.Context, id domain.SourceID) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `SELECT id FROM activity_sources WHERE id = $1 FOR UPDATE`, id.UUID())
	return err
}

// ListPersonalRecords returns the user's all-time best per (activity type,
// metric, window) across every activity. DISTINCT ON keeps only the best row
// per group; "best" is ASC for the domain's smaller-is-better metrics and DESC
// for every other metric, via a sign flip. Always current — it
// reads live best_efforts, so a new best automatically becomes the PR with no
// invalidation step.
func (r *BestEffortRepo) ListPersonalRecords(ctx context.Context, userID domain.UserID) ([]domain.PersonalRecord, error) {
	db := dbtx(ctx, r.pool)
	rows, err := db.Query(ctx, `
		SELECT DISTINCT ON (activity_type, metric, window_kind, window_value)
		       activity_type, metric, window_kind, window_value,
		       achieved_value, activity_id, ts
		  FROM best_efforts b
		 WHERE user_id = $1 AND `+recordVariantSQL(ctx)+` AND `+recordDistanceSQL+` AND `+validCyclingMotionRecordSQL+`
		   AND EXISTS (SELECT 1 FROM activities a WHERE a.id = b.activity_id AND a.deleted_at IS NULL AND a.type = b.activity_type AND `+canonicalEffortSourceSQL("b.")+communityReadSQL(ctx, "a.")+communityRecordSQL(ctx, "a.")+`)
		 ORDER BY activity_type, metric, window_kind, window_value,
		          (CASE WHEN metric = ANY($2) THEN achieved_value ELSE -achieved_value END) ASC,
		          ts ASC`,
		userID.UUID(), smallerIsBetterMetrics(),
	)
	if err != nil {
		return nil, fmt.Errorf("list personal records: %w", err)
	}
	defer rows.Close()
	var out []domain.PersonalRecord
	for rows.Next() {
		var (
			at, metric, wk string
			wv             int
			av             float64
			aid            uuid.UUID
			ts             time.Time
		)
		if err := rows.Scan(&at, &metric, &wk, &wv, &av, &aid, &ts); err != nil {
			return nil, fmt.Errorf("scan personal record: %w", err)
		}
		out = append(out, domain.PersonalRecord{
			ActivityType:  domain.ActivityType(at),
			Metric:        domain.BestEffortMetric(metric),
			WindowKind:    domain.BestEffortWindowKind(wk),
			WindowValue:   wv,
			AchievedValue: av,
			ActivityID:    domain.ActivityID(aid),
			Timestamp:     ts,
		})
	}
	return out, rows.Err()
}

func NewBestEffortRepo(pool *pgxpool.Pool) *BestEffortRepo {
	return &BestEffortRepo{pool: pool}
}

const bestEffortColumns = `
	id, activity_id, activity_source_id, user_id,
	activity_type, discipline,
	metric, window_kind, window_value,
	achieved_value,
	start_offset, duration_s, distance_m,
	ts, created_at
`

// DeleteForSource removes all best_efforts rows for the source.
func (r *BestEffortRepo) DeleteForUserWindow(ctx context.Context, userID domain.UserID, activityType domain.ActivityType, metric domain.BestEffortMetric, kind domain.BestEffortWindowKind, value int) error {
	db := dbtx(ctx, r.pool)
	_, err := db.Exec(ctx,
		`DELETE FROM best_efforts
		  WHERE user_id = $1 AND activity_type = $2 AND metric = $3 AND window_kind = $4 AND window_value = $5`,
		userID.UUID(), string(activityType), string(metric), string(kind), value)
	if err != nil {
		return fmt.Errorf("delete best_efforts window: %w", err)
	}
	return nil
}

func (r *BestEffortRepo) DeleteForSource(ctx context.Context, sourceID domain.SourceID) error {
	db := dbtx(ctx, r.pool)
	_, err := db.Exec(ctx,
		`DELETE FROM best_efforts WHERE activity_source_id = $1`,
		sourceID.UUID(),
	)
	if err != nil {
		return fmt.Errorf("delete best_efforts for %s: %w", sourceID, err)
	}
	return nil
}

// SaveEfforts inserts a batch of best_efforts in one pgx.Batch round trip.
func (r *BestEffortRepo) SaveEfforts(ctx context.Context, efforts []domain.BestEffort) error {
	return r.saveEfforts(ctx, efforts, "")
}

// SaveMissingEfforts adds historical windows without replacing results that
// an import or another backfill already computed.
func (r *BestEffortRepo) SaveMissingEfforts(ctx context.Context, efforts []domain.BestEffort) error {
	return r.saveEfforts(ctx, efforts, ` ON CONFLICT (activity_source_id, metric, window_kind, window_value, downhill_excluded) DO NOTHING`)
}

func (r *BestEffortRepo) saveEfforts(ctx context.Context, efforts []domain.BestEffort, onConflict string) error {
	if len(efforts) == 0 {
		return nil
	}
	db := dbtx(ctx, r.pool)

	batch := &pgx.Batch{}
	for _, e := range efforts {
		var disciplineParam any
		if e.Discipline != "" && e.Discipline != domain.DisciplineNone {
			disciplineParam = string(e.Discipline)
		}
		var createdAtParam any
		if !e.CreatedAt.IsZero() {
			createdAtParam = e.CreatedAt
		}

		batch.Queue(
			`INSERT INTO best_efforts (
				id, activity_id, activity_source_id, user_id,
				activity_type, discipline,
				metric, window_kind, window_value,
				achieved_value,
				start_offset, duration_s, distance_m,
				ts, created_at, downhill_excluded
			) VALUES (
				$1, $2, $3, $4,
				$5, $6,
				$7, $8, $9,
				$10,
				$11, $12, $13,
				$14, COALESCE($15, now()), $16
			)`+onConflict,
			e.ID.UUID(), e.ActivityID.UUID(), e.ActivitySourceID.UUID(), e.UserID.UUID(),
			string(e.ActivityType), disciplineParam,
			string(e.Metric), string(e.WindowKind), e.WindowValue,
			e.AchievedValue,
			e.StartOffset, e.DurationS, e.DistanceM,
			e.Timestamp, createdAtParam, e.DownhillExcluded,
		)
	}

	br := db.(pgxBatchExecer).SendBatch(ctx, batch)
	defer func() { _ = br.Close() }()
	for i := 0; i < len(efforts); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("save best_effort %d: %w", i, err)
		}
	}
	return nil
}

// ListForActivity returns every best_effort for the activity, ordered
// metric → window_value so the UI's power-curve / pace-curve renderer
// gets adjacent windows next to each other.
func (r *BestEffortRepo) ListForActivity(ctx context.Context, activityID domain.ActivityID) ([]domain.BestEffort, error) {
	db := dbtx(ctx, r.pool)
	rows, err := db.Query(ctx,
		`SELECT DISTINCT ON (b.metric, b.window_kind, b.window_value) `+prefixedColumns(bestEffortColumns, "b.")+`
		   FROM best_efforts b JOIN activities a ON a.id = b.activity_id
		  WHERE b.activity_id = $1 AND NOT b.downhill_excluded AND `+recordDistanceSQL+` AND `+canonicalEffortSourceSQL("b.")+communityReadSQL(ctx, "a.")+communityRecordSQL(ctx, "a.")+`
		  ORDER BY b.metric, b.window_kind, b.window_value,
		           CASE WHEN b.metric = ANY($2) THEN b.achieved_value ELSE -b.achieved_value END, b.ts, b.id`,
		activityID.UUID(), smallerIsBetterMetrics(),
	)
	if err != nil {
		return nil, fmt.Errorf("list best_efforts for %s: %w", activityID, err)
	}
	return pgCollectRows(rows, scanBestEffortRow, "scan best_effort row", "iterate best_effort rows")
}

// pgxBatchExecer is the subset of *pgxpool.Pool and pgx.Tx needed for
// SendBatch. Used as a local interface so the DBTX surface stays small.
type pgxBatchExecer interface {
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

func scanBestEffortRow(row rowScanner) (domain.BestEffort, error) {
	var (
		id, activityID, sourceID, userID uuid.UUID
		activityType                     string
		discipline                       *string
		metric, windowKind               string
		windowValue                      int
		achievedValue                    float64
		startOffset                      int
		durationS                        float64
		distanceM                        *float64
		ts, createdAt                    time.Time
	)
	if err := row.Scan(
		&id, &activityID, &sourceID, &userID,
		&activityType, &discipline,
		&metric, &windowKind, &windowValue,
		&achievedValue,
		&startOffset, &durationS, &distanceM,
		&ts, &createdAt,
	); err != nil {
		return domain.BestEffort{}, err
	}

	be := domain.BestEffort{
		ID:               domain.BestEffortID(id),
		ActivityID:       domain.ActivityID(activityID),
		ActivitySourceID: domain.SourceID(sourceID),
		UserID:           domain.UserID(userID),
		ActivityType:     domain.ActivityType(activityType),
		Metric:           domain.BestEffortMetric(metric),
		WindowKind:       domain.BestEffortWindowKind(windowKind),
		WindowValue:      windowValue,
		AchievedValue:    achievedValue,
		StartOffset:      startOffset,
		DurationS:        durationS,
		DistanceM:        distanceM,
		Timestamp:        ts,
		CreatedAt:        createdAt,
	}
	if discipline != nil {
		be.Discipline = domain.Discipline(*discipline)
	}
	return be, nil
}

// ListBestEffortHistory returns the user's value for one best-effort bucket
// (metric + window_kind + window_value) across every activity — the
// progression series. When an activity has the bucket on multiple sources, the
// better value wins (min for pace, max otherwise). Sorted chronologically.
func (r *BestEffortRepo) ListBestEffortHistory(
	ctx context.Context,
	userID domain.UserID,
	activityType, metric, windowKind string,
	windowValue int,
) ([]domain.BestEffortHistoryItem, error) {
	betterDir := "DESC" // higher achieved_value is better for power/speed/hr/vam
	if metric == "pace" {
		betterDir = "ASC" // pace stores seconds/km — lower is better
	}
	db := dbtx(ctx, r.pool)
	// Scope by activity_type: a "400 m pace" PR for running and cycling are
	// different efforts — mixing them produces a meaningless series.
	rows, err := db.Query(ctx,
		`SELECT DISTINCT ON (b.activity_id)
		        b.activity_id, a.title, a.start_time, b.achieved_value
		   FROM best_efforts b
		   JOIN activities a ON a.id = b.activity_id AND a.deleted_at IS NULL AND a.type = b.activity_type
		  WHERE b.user_id = $1 AND b.activity_type = $2
		    AND b.metric = $3 AND b.window_kind = $4 AND b.window_value = $5 AND `+recordVariantSQL(ctx)+` AND `+recordDistanceSQL+`
		    AND `+validCyclingMotionRecordSQL+` AND `+canonicalEffortSourceSQL("b.")+communityReadSQL(ctx, "a.")+communityRecordSQL(ctx, "a.")+`
		  ORDER BY b.activity_id, b.achieved_value `+betterDir,
		userID.UUID(), activityType, metric, windowKind, windowValue,
	)
	if err != nil {
		return nil, fmt.Errorf("list best-effort history: %w", err)
	}
	defer rows.Close()

	var out []domain.BestEffortHistoryItem
	for rows.Next() {
		var (
			it domain.BestEffortHistoryItem
			id uuid.UUID
		)
		if err := rows.Scan(&id, &it.Title, &it.StartTime, &it.AchievedValue); err != nil {
			return nil, fmt.Errorf("scan best-effort history: %w", err)
		}
		it.ActivityID = domain.ActivityID(id)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartTime.Before(out[j].StartTime) })
	return out, nil
}

// smallerIsBetterMetrics is domain.SmallerIsBetterMetrics as a text[] parameter.
func smallerIsBetterMetrics() []string {
	ms := domain.SmallerIsBetterMetrics()
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = string(m)
	}
	return out
}

// canonicalPerformanceSourceSQL keeps provider-restricted sources out of
// activities with independent recordings.
const canonicalPerformanceSourceSQL = `src.status <> 'detached' AND (src.provider <> 'strava' OR a.provider_restricted)`

// canonicalEffortSourceSQL also excludes stale or detached efforts.
func canonicalEffortSourceSQL(e string) string {
	return `EXISTS (SELECT 1 FROM activity_sources src
		WHERE src.id = ` + e + `activity_source_id AND src.activity_id = a.id
		  AND ` + canonicalPerformanceSourceSQL + `)`
}

const recordDistanceSQL = "(b.window_kind <> 'distance' OR b.window_value NOT IN (400, 1609, 1610, 21097, 21100, 42195, 42200))"

// Legacy efforts may predate GPS validation. Apply the cycling speed cap
// before ranking either variant, allowing for floating-point interpolation.
var validCyclingMotionRecordSQL = fmt.Sprintf(`(b.activity_type <> 'ride' OR b.metric NOT IN ('speed', 'pace') OR (
	b.achieved_value > 0 AND b.achieved_value < 'Infinity'::float8 AND CASE b.metric
		WHEN 'speed' THEN b.achieved_value <= %[1]f
		WHEN 'pace' THEN b.achieved_value >= 1000 / %[1]f
	END))`, domain.CyclingMaxSpeedMps+0.000001)

func recordVariantSQL(ctx context.Context) string {
	if port.DownhillsExcluded(ctx) {
		return "b.downhill_excluded = (b.activity_type = 'ride' AND b.metric IN ('speed', 'pace'))"
	}
	return "NOT b.downhill_excluded"
}
