//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tormoder/fit"

	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/postgres"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/export"
	gearuc "github.com/opencairn-org/cairn-core/internal/usecase/gear"
)

func TestAPI_CyclingDistanceWithSparseGarminFIT(t *testing.T) {
	env := newITestEnv(t)
	ctx := t.Context()
	rider := env.user("sparse-fit", domain.UserRoleUser)
	control := env.user("regular-fit", domain.UserRoleUser)
	bike, err := env.app.Gear.Create(ctx, rider.userID, gearuc.CreateInput{Kind: domain.GearKindBike, Name: "Road bike"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.app.Gear.SetDefault(ctx, rider.userID, domain.ActivityTypeRide, bike.ID); err != nil {
		t.Fatal(err)
	}
	const total = 10200.0
	start := time.Date(2025, 11, 6, 8, 0, 0, 0, time.UTC)
	elapsed := 1020 * time.Second
	original := domain.Activity{
		Type: domain.ActivityTypeRide, StartTime: start, EndTime: start.Add(elapsed),
		ElapsedDuration: elapsed, MovingDuration: elapsed - time.Second,
		Summary: domain.ActivitySummary{DistanceM: new(total)},
	}
	importRide := func(who *itestClient, intervals []int) domain.Activity {
		t.Helper()
		var samples []domain.StreamSample
		for seconds, i := 0, 0; ; i++ {
			metres := float64(seconds) * 10
			samples = append(samples, domain.StreamSample{
				Timestamp: start.Add(time.Duration(seconds) * time.Second), DistanceM: new(metres),
				Latitude: new(0.0), Longitude: new(12 + metres*180/(math.Pi*domain.EarthRadiusMeters)),
			})
			if seconds == int(elapsed.Seconds()) {
				break
			}
			seconds += intervals[i%len(intervals)]
		}
		raw, err := export.FIT(original, domain.Stream{Samples: samples})
		if err != nil {
			t.Fatal(err)
		}
		id := importGarminFITFixture(t, env, who, start, raw)
		act, err := env.app.Activities.GetActivity(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return act
	}
	sparse := importRide(rider, []int{1, 2, 3, 2, 2, 7, 8, 9})
	regular := importRide(control, []int{1, 2, 3})
	assertSummary := func(act domain.Activity, want float64) {
		t.Helper()
		if act.MovingDuration != original.MovingDuration || domain.Deref(act.Summary.DistanceM) != want ||
			domain.Deref(act.Summary.AvgSpeedMps) != want/original.MovingDuration.Seconds() ||
			domain.Deref(act.Summary.MaxSpeedMps) != 10 {
			t.Fatalf("distance = %v, average speed = %v, max speed = %v, moving time = %v",
				domain.Deref(act.Summary.DistanceM), domain.Deref(act.Summary.AvgSpeedMps),
				domain.Deref(act.Summary.MaxSpeedMps), act.MovingDuration)
		}
	}
	assertSummary(sparse, total)
	assertSummary(regular, total)
	assertRecords := func(act domain.Activity, want1km bool) []domain.BestEffort {
		t.Helper()
		if err := env.app.ComputeBestEfforts.Recalculate(ctx, *act.PrimaryStreamSourceID); err != nil {
			t.Fatal(err)
		}
		rows, err := env.app.BestEfforts.ListForActivity(ctx, act.ID)
		if err != nil {
			t.Fatal(err)
		}
		var got1km bool
		for _, row := range rows {
			got1km = got1km || (row.WindowKind == domain.BestEffortWindowDistance && row.WindowValue == 1000)
		}
		if got1km != want1km {
			t.Fatalf("1 km record present = %v, want %v", got1km, want1km)
		}
		return rows
	}
	records := assertRecords(sparse, false)
	assertRecords(regular, true)

	remerged, err := env.app.RecomputeActivity.Execute(ctx, sparse.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertSummary(remerged.Activity, total)

	seedLegacyDistance := func() {
		t.Helper()
		if _, err := env.pool.Exec(ctx, "UPDATE activities SET distance_m = 3000, avg_speed_mps = 1 WHERE id = $1", sparse.ID.UUID()); err != nil {
			t.Fatal(err)
		}
	}
	seedLegacyDistance()
	if _, err := env.pool.Exec(ctx, "UPDATE gear SET total_distance_m = 3000 WHERE id = $1", bike.ID.UUID()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Updated cycling summary", "Cycling summary already current"} {
		var log bytes.Buffer
		cmd := newActivitiesCmd()
		cmd.SetOut(&log)
		cmd.SetArgs([]string{"repair-cycling-distance", sparse.ID.String()})
		if err := cmd.ExecuteContext(ctx); err != nil || !strings.Contains(log.String(), want) {
			t.Fatalf("repair output = %q, want %q, error %v", log.String(), want, err)
		}
	}
	if after, err := env.app.BestEfforts.ListForActivity(ctx, sparse.ID); err != nil || !reflect.DeepEqual(after, records) {
		t.Fatalf("single-ride repair changed records, error %v", err)
	}
	if updatedBike, err := env.app.Gear.Get(ctx, rider.userID, bike.ID); err != nil || updatedBike.TotalDistanceM != total {
		t.Fatalf("gear total = %v, want %v, error %v", updatedBike.TotalDistanceM, total, err)
	}
	assertCyclingDistanceViews(t, rider, sparse.ID, total, total/original.MovingDuration.Seconds())
	seedLegacyDistance()
	if _, err := recomputeHistoricalRecords(ctx, env.pool, nil, true); err != nil {
		t.Fatal(err)
	}
	assertCyclingDistanceViews(t, rider, sparse.ID, total, total/original.MovingDuration.Seconds())

	override := domain.ClassificationOverride{ActivityID: sparse.ID, DistanceM: new(9900.0)}
	if err := postgres.NewClassificationOverrideRepo(env.pool).Set(ctx, override); err != nil {
		t.Fatal(err)
	}
	remerged, err = env.app.RecomputeActivity.Execute(ctx, sparse.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertSummary(remerged.Activity, *override.DistanceM)
	if changed, err := repairCyclingDistance(ctx, env.pool, sparse.ID); err != nil || changed {
		t.Fatalf("repair with manual override changed = %v, error %v", changed, err)
	}
}

func assertCyclingDistanceViews(t *testing.T, rider *itestClient, id domain.ActivityID, distance, speed float64) {
	t.Helper()
	var feed struct {
		Activities []struct {
			ID       string  `json:"id"`
			Distance float64 `json:"distance_m"`
			Speed    float64 `json:"avg_speed_mps"`
		}
	}
	if err := json.Unmarshal(rider.do(http.MethodGet, "/api/activities/feed", nil, http.StatusOK), &feed); err != nil ||
		len(feed.Activities) != 1 || feed.Activities[0].ID != id.String() ||
		feed.Activities[0].Distance != distance || feed.Activities[0].Speed != speed {
		t.Fatalf("feed = %+v, error %v", feed, err)
	}
	detail := rider.post("/cairn.v1.ActivityService/GetActivity", map[string]any{"id": id.String()}, http.StatusOK)
	act, _ := detail["activity"].(map[string]any)
	summary, _ := act["summary"].(map[string]any)
	if summary["distanceM"] != distance || summary["avgSpeedMps"] != speed {
		t.Fatalf("detail summary = %+v", summary)
	}
	path := "/api/activities/" + id.String() + "/export."
	var tcx struct {
		Distance  float64   `xml:"Activities>Activity>Lap>DistanceMeters"`
		Distances []float64 `xml:"Activities>Activity>Lap>Track>Trackpoint>DistanceMeters"`
	}
	if err := xml.Unmarshal(rider.do(http.MethodGet, path+"tcx", nil, http.StatusOK), &tcx); err != nil ||
		tcx.Distance != distance || len(tcx.Distances) == 0 || tcx.Distances[len(tcx.Distances)-1] != distance {
		t.Fatalf("TCX distance = %v, %d track distances, error %v", tcx.Distance, len(tcx.Distances), err)
	}
	f, err := fit.Decode(bytes.NewReader(rider.do(http.MethodGet, path+"fit", nil, http.StatusOK)))
	if err != nil {
		t.Fatal(err)
	}
	recording, err := f.Activity()
	if err != nil || len(recording.Sessions) != 1 || recording.Sessions[0].GetTotalDistanceScaled() != distance {
		t.Fatalf("FIT session total is inconsistent, error %v", err)
	}
}
