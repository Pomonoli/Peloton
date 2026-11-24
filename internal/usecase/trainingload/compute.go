// Package trainingload computes the rolling training-load curves
// (CTL/ATL/TSB) from a user's per-activity TSS values and persists them
// as time-series rows in the metrics hypertable.
//
// Algorithm: standard exponentially-weighted moving averages over daily
// TSS totals.
//
//	CTL_today = CTL_yesterday + (TSS_today − CTL_yesterday) / 42
//	ATL_today = ATL_yesterday + (TSS_today − ATL_yesterday) / 7
//	TSB_today = CTL_today − ATL_today
//
// v1 simplifications (documented in README):
//
//   - Days are bucketed by UTC. Per-user timezones is a v2 item.
//   - Warm-up state is CTL=0, ATL=0 at the window start. The compute
//     pass is therefore most accurate when the window starts far enough
//     before any data of interest (≥6 weeks).
//   - TSS comes from the merged Activity's Summary.TSS field. Activities
//     without a TSS contribute 0 to that day's bucket.
package trainingload

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// ComputeTrainingLoadForUser walks a user's activities in [start, end],
// aggregates daily TSS, and emits CTL/ATL/TSB rows for every day in the
// window. Existing computed rows in the same window are wiped first so
// re-runs are idempotent.
type ComputeTrainingLoadForUser struct {
	activities port.ActivityReader
	metrics    port.MetricRepo
	tx         port.TxManager

	now   func() time.Time
	newID func() uuid.UUID
}

func NewComputeTrainingLoadForUser(
	activities port.ActivityReader,
	metrics port.MetricRepo,
	tx port.TxManager,
	now func() time.Time,
	newID func() uuid.UUID,
) *ComputeTrainingLoadForUser {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if newID == nil {
		newID = func() uuid.UUID {
			id, err := uuid.NewV7()
			if err != nil {
				return uuid.New()
			}
			return id
		}
	}
	return &ComputeTrainingLoadForUser{
		activities: activities,
		metrics:    metrics,
		tx:         tx,
		now:        now,
		newID:      newID,
	}
}

// Input identifies the user and the window. WarmUpDays prepends extra
// days before Start so the EWMA has time to stabilise before the
// caller-visible portion of the window begins. The use case clips
// negative WarmUpDays to 0.
type Input struct {
	UserID     domain.UserID
	Start      time.Time
	End        time.Time
	WarmUpDays int
}

// Result reports what the compute produced.
type Result struct {
	DaysComputed   int
	ActivitiesUsed int
	RowsWritten    int
	StartDay       time.Time
	EndDay         time.Time
}

// trainingLoadTSSByDay buckets TSS by UTC day. Activities without TSS contribute 0.
func trainingLoadTSSByDay(activities []domain.Activity) (map[time.Time]float64, int) {
	tssByDay := make(map[time.Time]float64, 128)
	activitiesUsed := 0
	for _, a := range activities {
		if a.Summary.TSS == nil {
			continue
		}
		day := truncateUTCDay(a.StartTime)
		tssByDay[day] += *a.Summary.TSS
		activitiesUsed++
	}
	return tssByDay, activitiesUsed
}

// trainingLoadRows walks the window day-by-day, applying the EWMA recurrence.
func (uc *ComputeTrainingLoadForUser) trainingLoadRows(userID domain.UserID, startDay, endDay time.Time, tssByDay map[time.Time]float64) ([]domain.Metric, int) {
	var (
		rows []domain.Metric
		now  = uc.now()
		days = 0
	)
	for d, load := range dailyLoad(startDay, endDay, tssByDay) {

		// Emit four metric rows per day. The external_id is
		// deterministic ("computed:YYYY-MM-DD") so re-runs upsert via
		// the partial unique index.
		externalID := "computed:" + d.Format("2006-01-02")
		base := domain.Metric{
			UserID:        userID,
			Timestamp:     d,
			PeriodSeconds: 86400,
			Provider:      domain.MetricProviderComputed,
			ExternalID:    externalID,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		rows = append(rows,
			withKeyAndValue(base, uc.newID, domain.MetricKeyTrainingLoadTSS, load.TSS),
			withKeyAndValue(base, uc.newID, domain.MetricKeyTrainingLoadCTL, load.CTL),
			withKeyAndValue(base, uc.newID, domain.MetricKeyTrainingLoadATL, load.ATL),
			withKeyAndValue(base, uc.newID, domain.MetricKeyTrainingLoadTSB, load.TSB),
		)
		days++
	}
	return rows, days
}

// dailyLoad walks the UTC days and applies the same recurrence for persisted and community reads.
func dailyLoad(start, end time.Time, tssByDay map[time.Time]float64) func(func(time.Time, domain.TrainingLoadDay) bool) {
	return func(yield func(time.Time, domain.TrainingLoadDay) bool) {
		var ctl, atl float64
		for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
			tss, hasTSS := tssByDay[day]
			ctl += (tss - ctl) / 42
			atl += (tss - atl) / 7
			if !yield(day, domain.TrainingLoadDay{TSS: tss, HasTSS: hasTSS, CTL: ctl, ATL: atl, TSB: ctl - atl}) {
				return
			}
		}
	}
}

// CommunitySeries reconstructs the curve from the activities this viewer may see.
func CommunitySeries(ctx context.Context, reader port.ActivityReader, userID domain.UserID, start, end time.Time) ([]domain.TrainingLoadDay, error) {
	startDay, endDay := truncateUTCDay(start), truncateUTCDay(end.Add(-time.Nanosecond))
	if start.After(startDay) {
		startDay = startDay.AddDate(0, 0, 1)
	}
	warmup := startDay.AddDate(0, 0, -180)
	activities, err := reader.ListActivitiesForUser(ctx, userID, warmup, endDay.AddDate(0, 0, 1).Add(-time.Nanosecond))
	if err != nil {
		return nil, err
	}
	tss, used := trainingLoadTSSByDay(activities)
	if used == 0 {
		return nil, nil
	}
	var days []domain.TrainingLoadDay
	for day, load := range dailyLoad(warmup, endDay, tss) {
		if !day.Before(startDay) {
			days = append(days, domain.TrainingLoadDay{Date: day.Format("2006-01-02"), CTL: domain.Round1(load.CTL),
				ATL: domain.Round1(load.ATL), TSB: domain.Round1(load.TSB), TSS: domain.Round1(load.TSS), HasTSS: load.HasTSS})
		}
	}
	return days, nil
}

// Execute runs the compute. Activities are loaded fresh each call; the
// previous-window state is not persisted, so consecutive calls with
// overlapping windows just rewrite the overlap.
func (uc *ComputeTrainingLoadForUser) Execute(ctx context.Context, in Input) (Result, error) {
	if in.End.Before(in.Start) {
		return Result{}, fmt.Errorf("training-load: end %s before start %s", in.End, in.Start)
	}
	if in.WarmUpDays < 0 {
		in.WarmUpDays = 0
	}

	startDay := truncateUTCDay(in.Start).AddDate(0, 0, -in.WarmUpDays)
	endDay := truncateUTCDay(in.End)

	// Pull activities covering the whole window (warm-up included). The
	// upper bound is the end of the end-day so activities ending at
	// 23:59 are included.
	activities, err := uc.activities.ListActivitiesForUser(ctx, in.UserID, startDay, endDay.Add(24*time.Hour))
	if err != nil {
		return Result{}, fmt.Errorf("list activities: %w", err)
	}

	tssByDay, activitiesUsed := trainingLoadTSSByDay(activities)
	rows, days := uc.trainingLoadRows(in.UserID, startDay, endDay, tssByDay)

	// Persist atomically: delete the prior computed window, then upsert
	// the fresh rows. Delete is bounded to the same window so prior data
	// outside it stays put.
	err = uc.tx.InTx(ctx, func(ctx context.Context) error {
		if err := uc.metrics.DeleteComputedForUser(
			ctx, in.UserID, domain.AllTrainingLoadKeys, startDay, endDay.Add(24*time.Hour),
		); err != nil {
			return err
		}
		return uc.metrics.SaveMetrics(ctx, rows)
	})
	if err != nil {
		return Result{}, fmt.Errorf("persist training load: %w", err)
	}

	return Result{
		DaysComputed:   days,
		ActivitiesUsed: activitiesUsed,
		RowsWritten:    len(rows),
		StartDay:       startDay,
		EndDay:         endDay,
	}, nil
}

// withKeyAndValue clones the base metric with a key, a scalar value, and
// a freshly-minted ID. Centralises the "emit one of four daily rows"
// pattern so the recurrence loop stays compact.
func withKeyAndValue(
	base domain.Metric,
	newID func() uuid.UUID,
	key string,
	value float64,
) domain.Metric {
	v := value
	base.ID = domain.MetricID(newID())
	base.Key = key
	base.ValueNumeric = &v
	return base
}

// truncateUTCDay drops the time-of-day, returning the UTC midnight of
// the day containing t. Used to bucket TSS deterministically.
func truncateUTCDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}
