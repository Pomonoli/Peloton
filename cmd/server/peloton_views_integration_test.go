//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
	"github.com/opencairn-org/cairn-core/internal/usecase/dashboard"
)

func TestAPI_PelotonRiderViews(t *testing.T) {
	env := newITestEnv(t)
	ada := env.user("ada", domain.UserRoleUser)
	mira := env.user("mira", domain.UserRoleUser)
	monday, _ := dashboard.WeekBounds(time.Now())
	ride := func(owner *itestClient, sport domain.ActivityType, started time.Time, distance *float64, moving time.Duration) {
		t.Helper()
		if _, err := env.app.IngestActivity.Execute(t.Context(), activity.IngestInput{
			UserID: owner.userID, Provider: "manual", SourceWorkerName: "test", ExternalID: started.String(),
			Payload: domain.ActivitySourcePayload{
				Type: sport, StartTime: started, EndTime: started.Add(time.Hour), Timezone: "UTC",
				ElapsedDuration: time.Hour, MovingDuration: moving, Summary: domain.ActivitySummary{DistanceM: distance},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	ride(mira, domain.ActivityTypeRide, monday.Add(-time.Second), new(30_000.0), time.Hour)
	ride(mira, domain.ActivityTypeRide, monday, new(10_000.0), 30*time.Minute)
	ride(mira, domain.ActivityTypeRide, monday.AddDate(0, 0, -21), new(5_000.0), 0)
	ride(mira, domain.ActivityTypeWorkout, monday.Add(time.Minute), nil, 5*time.Minute)
	ride(ada, domain.ActivityTypeRide, monday, new(99_000.0), time.Hour)

	var overview struct {
		Weeks []struct {
			Start    string
			Distance float64 `json:"distance_m"`
		} `json:"cycling_weeks"`
		Recent []struct {
			Distance float64 `json:"distance_m"`
			Moving   float64 `json:"moving_duration_s"`
		}
	}
	body := ada.do(http.MethodGet, "/api/overview?member=mira", nil, http.StatusOK)
	if err := json.Unmarshal(body, &overview); err != nil {
		t.Fatal(err)
	}
	if len(overview.Weeks) != 12 || overview.Weeks[0].Start != monday.AddDate(0, 0, -77).Format("2006-01-02") ||
		overview.Weeks[8].Distance != 5000 || overview.Weeks[9].Distance != 0 ||
		overview.Weeks[10].Distance != 30000 || overview.Weeks[11].Distance != 10000 ||
		len(overview.Recent) != 4 || overview.Recent[1].Distance != 10000 || overview.Recent[1].Moving != 1800 {
		t.Fatalf("bad rider overview: %s", body)
	}
	var stats struct {
		Sports []struct {
			Value    string
			Count    int
			Distance float64  `json:"distance_m"`
			Moving   float64  `json:"moving_s"`
			Speed    *float64 `json:"speed_kmh"`
		} `json:"sport_totals"`
	}
	body = ada.do(http.MethodGet, "/api/stats?member=mira", nil, http.StatusOK)
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatal(err)
	}
	if len(stats.Sports) != 2 || stats.Sports[0].Value != "ride" || stats.Sports[0].Count != 3 ||
		stats.Sports[0].Distance != 45000 || stats.Sports[0].Moving != 5400 ||
		stats.Sports[0].Speed == nil || math.Abs(*stats.Sports[0].Speed-26.66667) > .001 || stats.Sports[1].Speed != nil {
		t.Fatalf("bad sport totals: %s", body)
	}
	ada.do(http.MethodGet, "/api/riders", nil, http.StatusOK)
	for _, path := range []string{"/api/health?type=HRV&member=mira", "/api/athlete/metrics?member=mira"} {
		ada.do(http.MethodGet, path, nil, http.StatusForbidden)
		mira.do(http.MethodGet, path, nil, http.StatusOK)
	}
	var analysis map[string]any
	if err := json.Unmarshal(ada.do(http.MethodGet, "/api/analysis?member=mira", nil, http.StatusOK), &analysis); err != nil {
		t.Fatal(err)
	}
	if coverage, ok := analysis["coverage"].(map[string]any); !ok || coverage["missing"].(float64)+coverage["duration_estimate"].(float64) != 4 {
		t.Fatalf("bad training data coverage: %v", analysis)
	}
}

func TestAPI_DirectionalSimilarRoutes(t *testing.T) {
	env := newITestEnv(t)
	rider := env.user("route-rider", domain.UserRoleUser)
	start := time.Date(2025, 3, 3, 9, 0, 0, 0, time.UTC)
	sample := func(at time.Time, northM, eastM float64) domain.StreamSample {
		degrees := 180 / (math.Pi * domain.EarthRadiusMeters)
		return domain.StreamSample{Timestamp: at, Latitude: new(51 + northM*degrees), Longitude: new(4 + eastM*degrees)}
	}
	record := func(day, seconds int, distance float64, reverse, detour bool) activity.IngestResult {
		t.Helper()
		from := start.AddDate(0, 0, day)
		samples := make([]domain.StreamSample, seconds+1)
		for i := range samples {
			frac := float64(i) / float64(seconds)
			north, east := distance*frac, 0.0
			if reverse {
				north = distance - north
			}
			if detour {
				east = math.Sin(frac*math.Pi) * 300
			}
			samples[i] = sample(from.Add(time.Duration(i)*time.Second), north, east)
		}
		duration := time.Duration(seconds) * time.Second
		out, err := env.app.IngestActivity.Execute(t.Context(), activity.IngestInput{
			UserID: rider.userID, Provider: "upload", SourceWorkerName: "test", ExternalID: fmt.Sprint(day),
			Payload: domain.ActivitySourcePayload{
				Type: domain.ActivityTypeRide, StartTime: from, EndTime: from.Add(duration), HasStream: true,
				MovingDuration: duration, ElapsedDuration: duration, Timezone: "UTC",
				Summary: domain.ActivitySummary{DistanceM: &distance},
			},
			Stream: samples,
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	reference := record(0, 120, 1200, false, false)
	shorter := record(1, 105, 1050, false, false)
	faster := record(2, 116, 1400, false, false)
	record(3, 120, 1200, true, false)
	record(4, 120, 1200, false, true)
	record(5, 60, 600, false, false)
	type similarPayload struct {
		Activities []struct {
			ID      string
			Overlap float64 `json:"overlap_pct"`
		}
		Summary struct {
			Rank      int     `json:"current_rank"`
			BestSpeed float64 `json:"best_speed_kmh"`
		}
	}
	read := func() similarPayload {
		t.Helper()
		var data similarPayload
		body := rider.do(http.MethodGet, "/api/activities/"+reference.ActivityID.String()+"/similar", nil, http.StatusOK)
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	group := read()
	if len(group.Activities) != 3 || group.Activities[1].ID != shorter.ActivityID.String() ||
		group.Activities[1].Overlap < .8 || group.Activities[2].ID != faster.ActivityID.String() ||
		group.Activities[2].Overlap < .8 || group.Summary.Rank < 2 || group.Summary.BestSpeed < 42 {
		t.Fatalf("unexpected route comparisons: %+v", group)
	}
	// Replacing a recording increments its stream revision. Cached route
	// coverage must immediately stop matching its previous geometry.
	away := []domain.StreamSample{sample(start, 0, 10_000), sample(start.Add(time.Second), 15, 10_000)}
	if err := env.app.Streams.WriteStream(t.Context(), faster.SourceID, away); err != nil {
		t.Fatal(err)
	}
	if updated := read(); len(updated.Activities) != 2 || updated.Summary.BestSpeed > 37 {
		t.Fatalf("stale match after stream replacement: %+v", updated)
	}
}

func TestAPI_ElevationVerifiedRecords(t *testing.T) {
	env := newITestEnv(t)
	rider := env.user("descent", domain.UserRoleUser)
	start := time.Date(2025, 6, 2, 9, 0, 0, 0, time.UTC)
	samples := make([]domain.StreamSample, 201)
	for i := range samples {
		metres := float64(i) * 15
		elevation := 80 - metres*.06
		if i >= 100 {
			metres = 1500 + float64(i-100)*8
			elevation = -10 + float64(i%3-1)*.5
		}
		samples[i] = domain.StreamSample{
			Timestamp: start.Add(time.Duration(i) * time.Second), RecordingIndex: int32(i + 1),
			Latitude: new(51 + metres*180/(math.Pi*domain.EarthRadiusMeters)), Longitude: new(4.0),
			DistanceM: &metres, AltitudeM: &elevation,
		}
	}
	ride, err := env.app.IngestActivity.Execute(t.Context(), activity.IngestInput{
		UserID: rider.userID, Provider: "upload", SourceWorkerName: "test", ExternalID: "ride",
		Payload: domain.ActivitySourcePayload{
			Type: domain.ActivityTypeRide, StartTime: start, EndTime: samples[len(samples)-1].Timestamp,
			HasStream: true, Timezone: "UTC", MovingDuration: 200 * time.Second, ElapsedDuration: 200 * time.Second,
		},
		Stream: samples,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.app.ComputeBestEfforts.Recalculate(t.Context(), ride.SourceID); err != nil {
		t.Fatal(err)
	}
	const endpoint = "/api/records/leaderboards/ride/speed/duration/60"
	check := func(path string, wantMin, wantMax float64) float64 {
		t.Helper()
		rows := readRecordBoard(t, rider, path)
		if len(rows) != 1 || rows[0].Result.AchievedValue < wantMin || rows[0].Result.AchievedValue > wantMax {
			t.Fatalf("bad speed record at %s: %+v", path, rows)
		}
		return rows[0].Result.AchievedValue
	}
	check(endpoint, 14.9, 15.1)
	check(endpoint+"?exclude_downhills=true", 7.9, 9)
	rider.do(http.MethodGet, "/api/records/leaderboards/ride/pace/distance/400", nil, http.StatusBadRequest)

	// Rebuild a historical source that predates the filtered variant.
	if _, err := env.pool.Exec(t.Context(), "DELETE FROM best_efforts WHERE activity_source_id = $1 AND downhill_excluded", ride.SourceID.UUID()); err != nil {
		t.Fatal(err)
	}
	rebuild, err := recomputeHistoricalRecords(t.Context(), env.pool, nil, true)
	if err != nil || rebuild.activities != 1 {
		t.Fatalf("failed to rebuild historical variants: %+v, %v", rebuild, err)
	}
	filtered := check(endpoint+"?exclude_downhills=true", 7.9, 9)
	var personal struct {
		Records []struct {
			Metric string
			Window int     `json:"window_value"`
			Value  float64 `json:"achieved_value"`
		}
	}
	body := rider.do(http.MethodGet, "/api/records?exclude_downhills=true", nil, http.StatusOK)
	if err := json.Unmarshal(body, &personal); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range personal.Records {
		if record.Metric == "speed" && record.Window == 60 && record.Value == filtered {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("personal records did not read the verified variant: %+v", personal.Records)
	}
	var history struct {
		Best  float64
		Count int
	}
	const historyPath = "/api/best-efforts/history?type=ride&metric=speed&window_kind=duration&window_value=60"
	body = rider.do(http.MethodGet, historyPath+"&exclude_downhills=true", nil, http.StatusOK)
	if err := json.Unmarshal(body, &history); err != nil || history.Count != 1 || history.Best != filtered {
		t.Fatalf("history did not read the verified variant: %+v, %v", history, err)
	}

	// Even stale rows written before GPS validation must not win a record.
	if _, err := env.pool.Exec(t.Context(), `UPDATE best_efforts SET achieved_value = 142.8 / 3.6
		WHERE activity_source_id = $1 AND metric = 'speed' AND window_value = 60 AND NOT downhill_excluded`, ride.SourceID.UUID()); err != nil {
		t.Fatal(err)
	}
	if got := readRecordBoard(t, rider, endpoint); len(got) != 0 {
		t.Fatalf("invalid legacy record reached the leaderboard: %+v", got)
	}
	body = rider.do(http.MethodGet, historyPath, nil, http.StatusOK)
	if err := json.Unmarshal(body, &history); err != nil || history.Count != 0 {
		t.Fatalf("invalid legacy record reached history: %+v, %v", history, err)
	}
}

type recordArchiveFixture struct {
	port.BlobStore
	key string
	raw []byte
}

func (a recordArchiveFixture) Stat(_ context.Context, key string) (port.BlobMeta, error) {
	if key != a.key {
		return port.BlobMeta{}, domain.ErrNotFound
	}
	return port.BlobMeta{SizeBytes: int64(len(a.raw))}, nil
}

func (a recordArchiveFixture) Get(_ context.Context, key string) ([]byte, string, error) {
	if key != a.key {
		return nil, "", domain.ErrNotFound
	}
	return a.raw, "application/gpx+xml", nil
}

func TestAPI_RecordRebuildRecoversArchive(t *testing.T) {
	env := newITestEnv(t)
	rider := env.user("archive", domain.UserRoleUser)
	ctx := t.Context()
	archive := recordArchiveFixture{
		key: "users/archive/recovery.gpx",
		raw: regexp.MustCompile(`<ele>[^<]*</ele>`).ReplaceAll(performanceGPX(time.Date(2022, 5, 2, 8, 0, 0, 0, time.UTC), 3*time.Second, 150, 280), nil),
	}
	uploaded := rider.upload("recovery.gpx", archive.raw)
	source, err := domain.ParseUUID[domain.SourceID](str(uploaded, "source_id"))
	if err != nil {
		t.Fatal(err)
	}
	if err := env.app.Activities.SetSourceRawBlob(ctx, source, archive.key, "application/gpx+xml", int64(len(archive.raw))); err != nil {
		t.Fatal(err)
	}
	if err := env.app.Streams.DeleteStream(ctx, source); err != nil {
		t.Fatal(err)
	}
	for _, restored := range []int{1, 0} {
		result, err := recomputeHistoricalRecords(ctx, env.pool, archive, false)
		if err != nil || result.restored != restored {
			t.Fatalf("historical recovery: %+v, %v", result, err)
		}
		rows := readRecordBoard(t, rider, "/api/records/leaderboards/ride/power/duration/300")
		if len(rows) != 1 || rows[0].Result == nil || rows[0].Result.AchievedValue != 280 {
			t.Fatalf("long power record missing from smart recording: %+v", rows)
		}
		rows = readRecordBoard(t, rider, "/api/records/leaderboards/ride/speed/duration/60?exclude_downhills=true")
		if len(rows) != 0 {
			t.Fatalf("ride without elevation was classified as verified: %+v", rows)
		}
	}
}
