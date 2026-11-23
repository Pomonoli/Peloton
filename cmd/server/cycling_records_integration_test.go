//go:build integration

package main

import (
	"math"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
)

func TestAPI_RecomputeHistoricalCyclingRecords(t *testing.T) {
	ctx := t.Context()
	env := newITestEnv(t)
	alice := env.user("historical-garmin", domain.UserRoleUser)
	bob := env.user("historical-upload", domain.UserRoleUser)
	start := time.Date(2021, 2, 1, 8, 0, 0, 0, time.UTC)

	importRide := func(c *itestClient, provider string, speed float64, glitch bool) activity.IngestResult {
		t.Helper()
		samples := make([]domain.StreamSample, 351)
		for i := range samples {
			seconds, metres := i, float64(i)*speed
			if glitch && i >= 175 {
				seconds += 9
				metres += 400 - speed
			}
			samples[i] = domain.StreamSample{
				Timestamp: start.Add(time.Duration(seconds) * time.Second), DistanceM: new(metres), SpeedMps: new(speed),
				Latitude: new(0.0), Longitude: new(12 + metres*180/(math.Pi*domain.EarthRadiusMeters)),
			}
		}
		if glitch {
			samples[70], samples[71] = samples[71], samples[70]
		}
		end := samples[len(samples)-1].Timestamp
		out, err := env.app.IngestActivity.Execute(ctx, activity.IngestInput{
			UserID: c.userID, Provider: provider, ExternalID: "historic", SourceWorkerName: "test",
			RawBlobID: "raw/archived.fit",
			Payload: domain.ActivitySourcePayload{
				Type: domain.ActivityTypeRide, StartTime: start, EndTime: end, HasStream: true,
				ElapsedDuration: end.Sub(start), MovingDuration: end.Sub(start), Timezone: "UTC",
				Summary: domain.ActivitySummary{DistanceM: samples[len(samples)-1].DistanceM},
			},
			Stream: samples,
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	a := importRide(alice, "garmin", 9, true)
	b := importRide(bob, "upload", 10, false)
	if err := env.app.ComputeBestEfforts.Recalculate(ctx, b.SourceID); err != nil {
		t.Fatal(err)
	}
	if err := env.app.BestEfforts.SaveEfforts(ctx, []domain.BestEffort{{
		ID: domain.BestEffortID(uuid.New()), ActivityID: a.ActivityID, ActivitySourceID: a.SourceID,
		UserID: alice.userID, ActivityType: domain.ActivityTypeRide, Metric: domain.BestEffortMetricPace,
		WindowKind: domain.BestEffortWindowDistance, WindowValue: 1000, AchievedValue: 25,
		DurationS: 25, DistanceM: new(1000.0), Timestamp: start,
	}}); err != nil {
		t.Fatal(err)
	}
	// Seed the legacy derived statistics as well.
	a.Activity.Summary.DistanceM, a.Activity.Summary.MaxSpeedMps = new(1741.0), new(40.0)
	if err := env.app.Activities.SaveActivity(ctx, a.Activity); err != nil {
		t.Fatal(err)
	}

	readRaw := func() domain.Stream {
		t.Helper()
		raw, err := env.app.Streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: a.SourceID, Resolution: domain.StreamResolutionRaw})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := readRaw()
	if len(raw.Samples) != 351 || raw.Samples[70].RecordingIndex != 72 || domain.ValidateCyclingStream(raw.Samples).Legs[70].Valid {
		t.Fatal("the timestamp-sorted database stream hid its original backwards sample")
	}

	assertPersonal := func(want float64) {
		t.Helper()
		rows, err := env.app.BestEfforts.ListPersonalRecords(ctx, alice.userID)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.ActivityType == domain.ActivityTypeRide && row.Metric == domain.BestEffortMetricPace && row.WindowValue == 1000 {
				if math.Abs(row.AchievedValue-want) > 0.001 || row.ActivityID != a.ActivityID {
					t.Fatalf("1 km personal record: %+v", row)
				}
				return
			}
		}
		t.Fatal("no 1 km personal record")
	}
	assertFirst := func(want domain.ActivityID) {
		t.Helper()
		rows := readRecordBoard(t, alice, "/api/records/leaderboards/ride/pace/distance/1000")
		if len(rows) != 2 || rows[0].Result.ActivityID != want.String() || rows[0].Result.Rank != 1 || rows[1].Result.Rank != 2 {
			t.Fatalf("1 km community board: %+v", rows)
		}
	}
	assertPersonal(25)
	assertFirst(a.ActivityID)

	for range 2 {
		cmd := newRecordsCmd()
		cmd.SetArgs([]string{"recompute-cycling"})
		if err := cmd.ExecuteContext(ctx); err != nil {
			t.Fatal(err)
		}
		assertPersonal(1000.0 / 9)
		assertFirst(b.ActivityID)
	}
	updated, err := env.app.Activities.GetActivity(ctx, a.ActivityID)
	if err != nil || updated.Summary.MaxSpeedMps == nil || *updated.Summary.MaxSpeedMps != 9 ||
		updated.Summary.DistanceM == nil || *updated.Summary.DistanceM >= 3150 {
		t.Fatalf("validated ride summary: %+v, %v", updated.Summary, err)
	}
	source, err := env.app.Activities.GetSource(ctx, a.SourceID)
	if err != nil || source.RawBlobID != "raw/archived.fit" || !reflect.DeepEqual(raw, readRaw()) {
		t.Fatalf("raw recording changed: %v", err)
	}
	alice.do(http.MethodGet, "/api/records/leaderboards", nil, http.StatusOK)
}
