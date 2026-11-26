//go:build integration

package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/config"
	"github.com/opencairn-org/cairn-core/internal/db"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
)

func stressInput(owner domain.UserID, provider, id string, start time.Time, moving time.Duration, summary domain.ActivitySummary) activity.IngestInput {
	return activity.IngestInput{UserID: owner, Provider: provider, ExternalID: id, SourceWorkerName: "test",
		Payload: domain.ActivitySourcePayload{Type: domain.ActivityTypeRide, StartTime: start,
			EndTime: start.Add(time.Hour), Timezone: "UTC", ElapsedDuration: time.Hour, MovingDuration: moving, Summary: summary}}
}

func ingestStress(t *testing.T, env *itestEnv, in activity.IngestInput) activity.IngestResult {
	t.Helper()
	result, err := env.app.IngestActivity.Execute(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertTrainingValue(t *testing.T, actual *float64, want float64) {
	t.Helper()
	if actual == nil || math.IsNaN(*actual) || math.Abs(*actual-want) > 1e-9 {
		if actual == nil {
			t.Fatalf("score is missing, want %v", want)
		}
		t.Fatalf("score = %v, want %v", *actual, want)
	}
}

func TestAPI_TrainingStressImportsAndRecordsRecompute(t *testing.T) {
	env := newITestEnv(t)
	ctx := t.Context()
	rider := env.user("stress-rider", domain.UserRoleUser)
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -900).Add(time.Hour)
	for _, entry := range []domain.AthleteMetricEntry{
		{UserID: rider.userID, Key: domain.AthleteFTPWatts, EffectiveDate: start.AddDate(0, 0, 1), Value: 200},
		{UserID: rider.userID, Key: domain.AthleteFTPWatts, EffectiveDate: start.AddDate(0, 0, 2), Value: 250},
		{UserID: rider.userID, Key: domain.AthleteThresholdHR, EffectiveDate: start, Value: 160},
	} {
		if _, err := env.app.AthleteProfiles.UpsertEntry(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	duration := ingestStress(t, env, stressInput(rider.userID, "upload", "duration", start, time.Hour, domain.ActivitySummary{}))
	if duration.Activity.Summary.TSS == nil || *duration.Activity.Summary.TSS != 49 {
		t.Fatalf("one hour without sensors should be exactly 49 TSS: %+v", duration.Activity.Summary)
	}
	assertTrainingValue(t, duration.Activity.Summary.IntensityFactor, 0.70)
	powerInput := stressInput(rider.userID, "garmin", "power", start.AddDate(0, 0, 1), time.Hour,
		domain.ActivitySummary{NormalizedPowerW: new(200), AvgPowerW: new(100), AvgHeartRateBpm: new(80)})
	power := ingestStress(t, env, powerInput)
	assertTrainingValue(t, power.Activity.Summary.TSS, 100)
	average := ingestStress(t, env, stressInput(rider.userID, "garmin", "average", start.AddDate(0, 0, 3), time.Hour,
		domain.ActivitySummary{AvgPowerW: new(125), AvgHeartRateBpm: new(80)}))
	assertTrainingValue(t, average.Activity.Summary.TSS, 27.5625)
	heart := ingestStress(t, env, stressInput(rider.userID, "strava", "heart", start.AddDate(0, 0, 4), time.Hour,
		domain.ActivitySummary{AvgHeartRateBpm: new(80)}))
	assertTrainingValue(t, heart.Activity.Summary.TSS, 25)
	imported := ingestStress(t, env, stressInput(rider.userID, "garmin", "supplied", start.AddDate(0, 0, 5), time.Hour,
		domain.ActivitySummary{TSS: new(83.0), IntensityFactor: new(0.91), NormalizedPowerW: new(200)}))
	assertTrainingValue(t, imported.Activity.Summary.TSS, 83)
	noDuration := ingestStress(t, env, stressInput(rider.userID, "manual", "no-duration", start.AddDate(0, 0, 6), 0, domain.ActivitySummary{}))
	if noDuration.Activity.Summary.TSS != nil {
		t.Fatal("moving time is required for an estimate")
	}
	invalid := stressInput(rider.userID, "upload", "invalid", start.AddDate(0, 0, 7), time.Hour,
		domain.ActivitySummary{TSS: new(-9.0)})
	repaired := ingestStress(t, env, invalid)
	assertTrainingValue(t, repaired.Activity.Summary.TSS, 49)

	// Reimport replaces the canonical sensor inputs. The new score uses the
	// original activity date and the same formula as the first ingest.
	powerInput.Payload.Summary.NormalizedPowerW = new(160)
	power = ingestStress(t, env, powerInput)
	assertTrainingValue(t, power.Activity.Summary.TSS, 64)
	if _, err := env.pool.Exec(ctx, `UPDATE activities SET tss = NULL WHERE id = $1`, duration.ActivityID.UUID()); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE activities SET tss = -10 WHERE id = $1`, power.ActivityID.UUID()); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE activities SET normalized_power_w = 100 WHERE id = $1`, average.ActivityID.UUID()); err != nil {
		t.Fatal(err)
	}
	result, err := recomputeHistoricalRecords(ctx, env.pool, nil, false)
	if err != nil || result.stressRepaired != 3 {
		t.Fatalf("records recompute = %+v, error %v", result, err)
	}
	for _, pair := range []struct {
		id    domain.ActivityID
		score float64
	}{
		{duration.ActivityID, 49}, {power.ActivityID, 64}, {average.ActivityID, 16}, {imported.ActivityID, 83},
	} {
		act, err := env.app.Activities.GetActivity(ctx, pair.id)
		if err != nil {
			t.Fatal(err)
		}
		assertTrainingValue(t, act.Summary.TSS, pair.score)
		if pair.id == imported.ActivityID {
			assertTrainingValue(t, act.Summary.IntensityFactor, 0.91)
		}
	}
	before, err := env.app.Stats.Analysis(ctx, rider.userID, 3650)
	if err != nil || before.Coverage != (port.TrainingStressCoverage{Imported: 1, SensorEstimate: 3, DurationEstimate: 2, Missing: 1}) {
		t.Fatalf("training stress coverage = %+v, error %v", before.Coverage, err)
	}
	assertFirstTrainingDay(t, env, rider.userID, start, 49)
}

func assertFirstTrainingDay(t *testing.T, env *itestEnv, rider domain.UserID, start time.Time, tss float64) {
	t.Helper()
	day := start.UTC().Truncate(24 * time.Hour)
	rows, err := env.app.Metrics.ListMetricsForUser(t.Context(), rider, "", day, day)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		domain.MetricKeyTrainingLoadTSS: tss, domain.MetricKeyTrainingLoadCTL: tss / 42,
		domain.MetricKeyTrainingLoadATL: tss / 7, domain.MetricKeyTrainingLoadTSB: tss/42 - tss/7,
	}
	if len(rows) != len(want) {
		t.Fatalf("first training day has %d rows, want %d", len(rows), len(want))
	}
	for _, row := range rows {
		if value, ok := want[row.Key]; ok {
			assertTrainingValue(t, row.ValueNumeric, value)
			delete(want, row.Key)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing metric rows: %v", want)
	}
}

func TestStartupTrainingStressBackfill(t *testing.T) {
	env := newITestEnv(t)
	ctx := t.Context()
	if _, err := db.MigrateDownTo(ctx, env.pool, 88); err != nil {
		t.Fatal(err)
	}
	first := env.user("first-stress-rider", domain.UserRoleUser)
	second := env.user("second-stress-rider", domain.UserRoleUser)
	if first.userID.String() > second.userID.String() {
		first, second = second, first
	}
	if _, err := env.pool.Exec(ctx, `UPDATE users SET status = 'suspended' WHERE id = $1`, second.userID.UUID()); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -830).Add(time.Hour)
	old := ingestStress(t, env, stressInput(first.userID, "upload", "first", start, time.Hour, domain.ActivitySummary{}))
	// Large enough to require a committed checkpoint before this rider's
	// history rebuild. Original source records are left in place.
	for i := range 100 {
		ingestStress(t, env, stressInput(first.userID, "manual", fmt.Sprintf("ride-%d", i),
			start.AddDate(0, 0, i+1), time.Hour, domain.ActivitySummary{}))
	}
	preserved := ingestStress(t, env, stressInput(second.userID, "garmin", "recorded", start, 0,
		domain.ActivitySummary{TSS: new(61.0)}))
	ingestStress(t, env, stressInput(second.userID, "manual", "not-scored", start.AddDate(0, 0, 1), 0, domain.ActivitySummary{}))
	if _, err := env.pool.Exec(ctx, `UPDATE activities SET tss = NULL, intensity_factor = NULL WHERE user_id = $1`, first.userID.UUID()); err != nil {
		t.Fatal(err)
	}
	source, err := env.app.Activities.GetSource(ctx, old.SourceID)
	if err != nil {
		t.Fatal(err)
	}

	if err := ensureSchema(ctx, testLogger(), env.pool, true); err != nil {
		t.Fatal(err)
	}
	backfill := *env.app.TrainingStressBackfill
	interrupted := backfill
	forced := errors.New("interrupted after processing part of a batch")
	entered, release := make(chan struct{}), make(chan struct{})
	var calls int
	interrupted.Repair = func(ctx context.Context, id domain.ActivityID) (activity.TrainingStressRepairResult, error) {
		calls++
		if calls == 2 {
			close(entered)
			<-release
			return activity.TrainingStressRepairResult{}, forced
		}
		return backfill.Repair(ctx, id)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := interrupted.Step(ctx)
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("batch stopped before its checkpoint: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("batch did not start")
	}
	_, advanced, err := backfill.Step(ctx)
	close(release)
	if err != nil || advanced {
		t.Fatalf("concurrent batch advanced: %v, error %v", advanced, err)
	}
	if err := <-done; !errors.Is(err, forced) {
		t.Fatalf("interruption: %v", err)
	}
	act, err := env.app.Activities.GetActivity(ctx, old.ActivityID)
	if err != nil || act.Summary.TSS != nil {
		t.Fatalf("interrupted batch committed a score: %v, error %v", act.Summary.TSS, err)
	}

	checkpoint, advanced, err := backfill.Step(ctx)
	if err != nil || !advanced || checkpoint.Scanned != 100 || checkpoint.Repaired != 100 ||
		checkpoint.Owner != first.userID || checkpoint.Activity == (domain.ActivityID{}) {
		t.Fatalf("first committed checkpoint = %+v, advanced %v, error %v", checkpoint, advanced, err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	serveStartBackgroundLoops(runCtx, &App{TrainingStressBackfill: env.app.TrainingStressBackfill}, &config.Config{}, testLogger())
	waitCtx, stop := context.WithTimeout(ctx, 12*time.Second)
	defer stop()
	var completed port.TrainingStressBackfillState
	for !completed.Completed {
		err := env.pool.QueryRow(waitCtx, `SELECT completed, scanned, repaired, missing, missing_with_duration FROM training_stress_backfill`).
			Scan(&completed.Completed, &completed.Scanned, &completed.Repaired, &completed.Missing, &completed.MissingWithDuration)
		if err != nil {
			t.Fatal(err)
		}
		if !completed.Completed {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if completed.Scanned != 103 || completed.Repaired != 101 || completed.Missing != 1 || completed.MissingWithDuration != 0 {
		t.Fatalf("startup completion = %+v", completed)
	}
	assertFirstTrainingDay(t, env, first.userID, start, 49)
	assertFirstTrainingDay(t, env, second.userID, start, 61)
	if act, err := env.app.Activities.GetActivity(ctx, preserved.ActivityID); err != nil {
		t.Fatal(err)
	} else {
		assertTrainingValue(t, act.Summary.TSS, 61)
	}
	if unchanged, err := env.app.Activities.GetSource(ctx, old.SourceID); err != nil || !reflect.DeepEqual(unchanged, source) {
		t.Fatalf("backfill rewrote original source: %+v, error %v", unchanged, err)
	}
	_, advanced, err = backfill.Step(ctx)
	if err != nil || advanced {
		t.Fatalf("completed backfill ran again: %v, error %v", advanced, err)
	}
}
