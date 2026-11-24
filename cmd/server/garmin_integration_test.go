//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/capability"
	"github.com/opencairn-org/cairn-core/internal/domain/health"
	"github.com/opencairn-org/cairn-core/internal/export"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/trainingload"
)

type fixtureGarminAPI struct {
	now   time.Time
	rides map[string][]domain.GarminActivity
	fits  map[string][]byte
}

func (f *fixtureGarminAPI) ExchangeKey(_ context.Context, key string) (domain.GarminSession, error) {
	if _, ok := f.rides[key]; !ok {
		return domain.GarminSession{}, port.ErrGarminUnauthorized
	}
	return domain.GarminSession{Key: key, Token: key, ExpiresAt: f.now.Add(24 * time.Hour)}, nil
}

func (*fixtureGarminAPI) ProfileHandle(_ context.Context, token string) (string, error) {
	return "profile-" + token, nil
}

func (f *fixtureGarminAPI) Rides(_ context.Context, token string, low, high time.Time, offset, limit int) ([]domain.GarminActivity, error) {
	var page []domain.GarminActivity
	for _, ride := range f.rides[token] {
		day := ride.LocalDay
		if day.Format(time.DateOnly) > high.Format(time.DateOnly) || !low.IsZero() && day.Format(time.DateOnly) < low.Format(time.DateOnly) {
			continue
		}
		if offset > 0 {
			offset--
			continue
		}
		page = append(page, ride)
		if len(page) == limit {
			break
		}
	}
	return page, nil
}

func (f *fixtureGarminAPI) Original(_ context.Context, token, id string) (port.UploadInput, error) {
	raw := f.fits[token+":"+id]
	if len(raw) == 0 {
		return port.UploadInput{}, port.ErrGarminNoFile
	}
	return port.UploadInput{Filename: id + ".fit", Raw: raw}, nil
}

type garminTestArchive struct {
	port.BlobStore
	data map[string][]byte
}

func (s *garminTestArchive) Put(_ context.Context, key string, raw []byte, _ string) error {
	s.data[key] = bytes.Clone(raw)
	return nil
}

type garminTestCatalog struct{ definition domain.Segment }

func (garminTestCatalog) Name() string { return "fixture-strava-catalog" }
func (p garminTestCatalog) Discover(_ context.Context, _ port.BoundingBox) (port.SegmentDiscoveryPage, error) {
	return port.SegmentDiscoveryPage{Segments: []domain.Segment{p.definition}}, nil
}

func garminTestFIT(t *testing.T, start time.Time) []byte {
	t.Helper()
	duration := 400 * time.Second
	samples := make([]domain.StreamSample, 0, 401)
	for i := range 401 {
		samples = append(samples, domain.StreamSample{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Latitude:  new(47.0 + float64(i)/20_000), Longitude: new(8.0 + float64(i)*0.00004),
			AltitudeM: new(125.0 + float64(i)/5), DistanceM: new(float64(i) * 6.35),
			HeartRateBpm: new(int16(143)), Cadence: new(int16(88)), PowerW: new(int16(225)),
		})
	}
	a := domain.Activity{Type: domain.ActivityTypeRide, StartTime: start, EndTime: start.Add(duration), ElapsedDuration: duration, MovingDuration: duration}
	raw, err := export.FIT(a, domain.Stream{Samples: samples})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAPI_GarminConnectHistoryAndSegmentDiscovery(t *testing.T) {
	env := newITDiscoveryEnv(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	oldRide := domain.GarminActivity{
		ID: "700", Type: "road_cycling", Title: "Garmin historical ride",
		StartTime: time.Date(2022, 9, 21, 12, 0, 0, 0, time.UTC), LocalDay: time.Date(2022, 9, 21, 0, 0, 0, 0, time.UTC),
		Elapsed: 400 * time.Second, Moving: 400 * time.Second,
	}
	summaryRide := oldRide
	summaryRide.ID, summaryRide.Title = "701", "Indoor ride"
	summaryRide.StartTime, summaryRide.LocalDay = oldRide.StartTime.AddDate(0, 0, -1), oldRide.LocalDay.AddDate(0, 0, -1)
	summaryRide.DistanceM = new(2600.0)
	original := garminTestFIT(t, oldRide.StartTime)
	api := &fixtureGarminAPI{
		now: now, rides: map[string][]domain.GarminActivity{
			"fixture-alice": {oldRide, summaryRide}, "fixture-bob": {oldRide},
		},
		fits: map[string][]byte{"fixture-alice:700": original, "fixture-bob:700": original},
	}
	env.app.Garmin.API = api
	env.app.Garmin.Now = func() time.Time { return now }
	archive := &garminTestArchive{data: make(map[string][]byte)}
	env.app.BlobStore = archive

	segmentID, provider := "123456789", "fixture-strava-catalog"
	def := domain.Segment{
		Source: domain.SegmentSourceStravaCatalog, ExternalID: &segmentID, CatalogProvider: &provider,
		Scope: domain.SegmentScopeInstance, Name: "Historical Garmin segment", ActivityType: domain.ActivityTypeRide,
		Polyline: "gt{}G_hzo@o}@_q@", PolylinePrecision: 5,
	}
	points, err := def.DecodedPolyline()
	if err != nil {
		t.Fatal(err)
	}
	def.DistanceM = domain.PolylineDistanceM(points)
	env.app.DiscoverSegments.Provider = garminTestCatalog{def}
	startITConsumers(t, env, startFollowUpConsumer)

	alice := env.user("garmin-alice", domain.UserRoleUser)
	bob := env.user("garmin-bob", domain.UserRoleUser)
	weight := alice.put("/api/athlete/metrics", map[string]any{
		"key": "weight_kg", "effective_date": "2021-01-01", "value": 73.5,
	}, http.StatusOK)
	alice.put("/api/athlete/metrics", map[string]any{
		"key": "ftp_watts", "effective_date": "2021-01-01", "value": 250,
	}, http.StatusOK)
	const path = "/api/connections/garmin"
	env.anon().do(http.MethodGet, path, nil, http.StatusUnauthorized)
	alice.do(http.MethodPut, path, map[string]string{"key": "invalid-fixture"}, http.StatusBadRequest)
	alice.do(http.MethodPut, path, map[string]string{"key": "fixture-alice"}, http.StatusNoContent)
	alice.do(http.MethodPut, path, map[string]string{"key": "fixture-bob"}, http.StatusConflict)
	rawStatus := bob.do(http.MethodGet, path, nil, http.StatusOK)
	var status struct {
		Connected       bool       `json:"connected"`
		HistoryComplete bool       `json:"history_complete"`
		HistoryRides    int        `json:"history_rides"`
		SummaryOnly     int        `json:"summary_only"`
		LastSync        *time.Time `json:"last_sync"`
	}
	if err := json.Unmarshal(rawStatus, &status); err != nil || status.Connected {
		t.Fatalf("Bob saw Alice's Garmin connection: %s", rawStatus)
	}

	runPage := func() {
		t.Helper()
		if ran, err := env.app.Garmin.RunOne(ctx); err != nil || !ran {
			t.Fatalf("Garmin batch ran = %v, error %v", ran, err)
		}
		now = now.Add(time.Second)
	}
	runPage()
	runPage()
	rawStatus = alice.do(http.MethodGet, path, nil, http.StatusOK)
	if err := json.Unmarshal(rawStatus, &status); err != nil || !status.Connected || !status.HistoryComplete ||
		status.HistoryRides != 2 || status.SummaryOnly != 1 || status.LastSync == nil || bytes.Contains(rawStatus, []byte("fixture-alice")) {
		t.Fatalf("unexpected history status: %s", rawStatus)
	}
	src, err := env.app.Activities.FindOwnedSourceByExternalID(ctx, alice.userID, garminProvider, nil, oldRide.ID)
	if err != nil || !bytes.Equal(archive.data[src.RawBlobID], original) || src.RawContentType != "application/vnd.ant.fit" {
		t.Fatalf("historical FIT archive missing: key %q, error %v", src.RawBlobID, err)
	}
	stream, err := env.app.Streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: src.ID, Resolution: domain.StreamResolutionRaw})
	if err != nil || len(stream.Samples) != 401 {
		t.Fatalf("historical FIT stream: %d samples, %v", len(stream.Samples), err)
	}
	first := stream.Samples[0]
	if !first.Timestamp.Equal(oldRide.StartTime) || first.Latitude == nil || first.Longitude == nil || first.AltitudeM == nil ||
		first.HeartRateBpm == nil || *first.HeartRateBpm != 143 || first.Cadence == nil || *first.Cadence != 88 ||
		first.PowerW == nil || *first.PowerW != 225 || *first.AltitudeM != 125 {
		t.Fatalf("historical recording fields lost: %+v", first)
	}
	if got := activitySegmentEfforts(t, bob, src.ActivityID.String()); len(got) != 0 {
		t.Fatalf("discovery ran before catch-up: %+v", got)
	}
	if _, err := env.app.DiscoverSegments.BackfillGarmin(ctx, domain.SourceID{}, 5); err != nil {
		t.Fatal(err)
	}
	if pending, err := env.app.Segments.ListPendingGarminSegmentScans(ctx, domain.SourceID{}, 5); err != nil || len(pending) != 0 {
		t.Fatalf("completed recordings or summary-only rides are still pending: %+v, %v", pending, err)
	}
	startITConsumers(t, env, startSegmentDiscoveryConsumer)
	efforts := waitForSegmentEfforts(t, alice, src.ActivityID.String(), 1)
	if math.Abs(efforts[0].Elapsed-200) > 0.1 || !efforts[0].PR || efforts[0].KOM != 1 {
		t.Fatalf("Garmin ride did not enter segment discovery and ranking: %+v", efforts)
	}
	stats, err := env.app.Stats.Get(ctx, alice.userID)
	if err != nil || stats.Totals.Count != 2 || stats.Totals.DistanceM < 5000 {
		t.Fatalf("historical Garmin rides missing from lifetime stats: %+v, %v", stats.Totals, err)
	}
	powerRecord := false
	for stop := time.Now().Add(5 * time.Second); time.Now().Before(stop); {
		records, err := env.app.Stats.PersonalRecords(ctx, alice.userID)
		if err != nil {
			t.Fatal(err)
		}
		powerRecord = slices.ContainsFunc(records, func(record domain.PersonalRecord) bool {
			return record.ActivityID == src.ActivityID && record.Metric == domain.BestEffortMetricPower &&
				record.WindowValue == 300 && record.AchievedValue == 225
		})
		if powerRecord {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !powerRecord {
		t.Fatal("historical Garmin power samples did not produce a personal record")
	}

	if _, err := env.app.Activities.FindOwnedSourceByExternalID(ctx, bob.userID, garminProvider, nil, oldRide.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Bob can see Alice's source: %v", err)
	}
	bob.do(http.MethodPut, path, map[string]string{"key": "fixture-bob"}, http.StatusNoContent)
	runPage()
	runPage()
	bobSource, err := env.app.Activities.FindOwnedSourceByExternalID(ctx, bob.userID, garminProvider, nil, oldRide.ID)
	if err != nil || bobSource.ID == src.ID || bobSource.ActivityID == src.ActivityID {
		t.Fatalf("same remote ID was not isolated: %v", err)
	}
	waitForSegmentEfforts(t, bob, bobSource.ActivityID.String(), 1)
	if board := segmentBoard(t, alice, efforts[0].SegmentID); len(board) != 2 {
		t.Fatalf("Garmin recordings did not enter the shared leaderboard: %+v", board)
	}
	bob.do(http.MethodDelete, path, map[string]any{}, http.StatusNoContent)

	// Import a newer ride automatically, after the cyclist already uploaded the
	// same FIT through the unchanged manual upload path.
	newRide := oldRide
	newRide.ID, newRide.Title = "702", "Garmin recent ride"
	newRide.StartTime = now.Add(-12 * time.Hour)
	newRide.LocalDay = newRide.StartTime.Truncate(24 * time.Hour)
	recentFIT := garminTestFIT(t, newRide.StartTime)
	manual := alice.upload("manual-before-garmin.fit", recentFIT)
	api.rides["fixture-alice"] = append([]domain.GarminActivity{newRide}, api.rides["fixture-alice"]...)
	api.fits["fixture-alice:702"] = recentFIT
	now = now.Add(time.Hour)
	runPage()
	runPage()
	newSource, err := env.app.Activities.FindOwnedSourceByExternalID(ctx, alice.userID, garminProvider, nil, newRide.ID)
	if err != nil || newSource.ActivityID.String() != str(manual, "activity_id") {
		t.Fatalf("manual and Garmin sources did not merge: %v", err)
	}

	if _, err := env.app.ComputeTrainingLoad.Execute(ctx, trainingload.Input{
		UserID: alice.userID, Start: newRide.StartTime.AddDate(0, 0, -30), End: now, WarmUpDays: 42,
	}); err != nil {
		t.Fatal(err)
	}
	verifyGarminCommunityReads(t, env, alice, bob, src, newSource, efforts[0].SegmentID, str(weight, "id"))

	alice.do(http.MethodPost, path+"/sync", map[string]any{}, http.StatusAccepted)
	runPage()
	runPage()
	repeated, err := env.app.Activities.FindOwnedSourceByExternalID(ctx, alice.userID, garminProvider, nil, newRide.ID)
	if err != nil || repeated.ID != newSource.ID {
		t.Fatalf("manual sync created a duplicate source: %v", err)
	}
	// Users can remove an imported ride without the next sync restoring it.
	if err := env.app.SourceActions.Delete(ctx, alice.userID, newSource.ActivityID); err != nil {
		t.Fatal(err)
	}
	alice.do(http.MethodPost, path+"/sync", map[string]any{}, http.StatusAccepted)
	runPage()
	runPage()
	if _, err := env.app.Activities.FindOwnedSourceByExternalID(ctx, alice.userID, garminProvider, nil, newRide.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Garmin sync restored a removed ride: %v", err)
	}
}

func verifyGarminCommunityReads(t *testing.T, env *itestEnv, alice, bob *itestClient, old, recent domain.ActivitySource, segmentID, metricID string) {
	t.Helper()
	activity := "/api/activities/" + old.ActivityID.String()
	segment := "/api/segments/" + segmentID
	query := "?member=" + url.QueryEscape(alice.name)
	const activityRPC = "/cairn.v1.ActivityService/"
	const gearRPC = "/cairn.v1.GearService/"
	alice.rpc(activityRPC+"UpdateActivity", map[string]any{
		"id": old.ActivityID.String(), "field_mask": []string{"privacy"}, "privacy": "PRIVACY_PRIVATE",
	}, http.StatusOK)
	bob.do(http.MethodGet, activity+"/zones", nil, http.StatusOK)
	bike := alice.post(gearRPC+"CreateGear", map[string]any{"kind": "GEAR_KIND_BIKE", "name": "Garmin Test Bike", "weightKg": 8.5}, http.StatusOK)["gear"].(map[string]any)
	gearID := str(bike, "id")
	alice.rpc(activityRPC+"UpdateActivity", map[string]any{
		"id": recent.ActivityID.String(), "field_mask": []string{"gear_id"}, "gear_id": gearID,
	}, http.StatusOK)
	assigned := "/api/activities/" + recent.ActivityID.String() + "/power-inputs"
	if body := alice.do(http.MethodGet, assigned, nil, http.StatusOK); !bytes.Contains(body, []byte(`"bicycle_weight_kg":8.5`)) {
		t.Fatalf("assigned bicycle weight missing: %s", body)
	}
	bob.do(http.MethodGet, assigned, nil, http.StatusNotFound)
	before, err := env.app.Activities.GetActivity(t.Context(), recent.ActivityID)
	if err != nil {
		t.Fatal(err)
	}
	alice.rpc(gearRPC+"UpdateGear", map[string]any{"id": gearID, "field_mask": []string{"weight_kg"}, "weight_kg": 12}, http.StatusOK)
	if body := alice.do(http.MethodGet, assigned, nil, http.StatusOK); !bytes.Contains(body, []byte(`"bicycle_weight_kg":12`)) {
		t.Fatalf("edited bicycle weight missing: %s", body)
	}
	after, err := env.app.Activities.GetActivity(t.Context(), recent.ActivityID)
	if err != nil || before.Summary.AvgPowerW == nil || after.Summary.AvgPowerW == nil || *before.Summary.AvgPowerW != *after.Summary.AvgPowerW {
		t.Fatalf("gear weight changed measured power: before=%v after=%v err=%v", before.Summary.AvgPowerW, after.Summary.AvgPowerW, err)
	}

	if err := env.app.Health.SaveSamples(t.Context(), alice.userID, nil, []health.Sample{{
		DataType: capability.DataTypeHRV, Timestamp: time.Now().Add(-time.Hour), Value: new(57.5), Unit: "ms", Provider: "garmin",
	}}); err != nil {
		t.Fatal(err)
	}
	requireBody := func(path, want string) []byte {
		t.Helper()
		body := bob.do(http.MethodGet, path, nil, http.StatusOK)
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("%s missing %q: %.600s", path, want, body)
		}
		return body
	}
	for _, path := range []string{"/api/athlete/metrics" + query, "/api/health" + query + "&type=HRV"} {
		bob.do(http.MethodGet, path, nil, http.StatusForbidden)
	}
	bob.do(http.MethodGet, activity+"/power-inputs", nil, http.StatusNotFound)
	if body := alice.do(http.MethodGet, "/api/athlete/metrics", nil, http.StatusOK); !bytes.Contains(body, []byte(metricID)) {
		t.Fatal("the owner could not read their athlete settings")
	}
	if body := alice.do(http.MethodGet, "/api/health?type=HRV", nil, http.StatusOK); !bytes.Contains(body, []byte(`"value":57.5`)) {
		t.Fatal("the owner could not read their health measurements")
	}
	if body := alice.do(http.MethodGet, activity+"/power-inputs", nil, http.StatusOK); !bytes.Contains(body, []byte(`"weight_kg":73.5`)) {
		t.Fatal("the owner could not read their archived weight")
	}
	if body := requireBody(activity+"/zones", `"power":{`); bytes.Contains(body, []byte(`"reference"`)) {
		t.Fatal("zone output disclosed the rider's thresholds")
	}

	for _, req := range []struct{ path, want string }{
		{"/api/members/" + alice.name, alice.userID.String()},
		{"/api/profiles/" + alice.name, old.ActivityID.String()},
		{"/api/overview" + query, recent.ActivityID.String()},
		{"/api/activities/feed" + query, old.ActivityID.String()},
		{"/api/stats" + query, `"year":2022`},
		{"/api/records" + query, `"achieved_value":225`},
		{"/api/best-efforts/history" + query + "&type=ride&metric=power&window_kind=duration&window_value=300", old.ActivityID.String()},
		{"/api/analysis" + query + "&days=30", `"ctl":[`},
		{"/api/activities/heatmap" + query, old.ActivityID.String()},
		{"/api/segments" + query, segmentID},
		{"/api/segments/catalog" + query + "&mine=true", segmentID},
		{segment + query, old.ActivityID.String()},
		{segment + "/efforts?user_id=" + alice.userID.String(), old.ActivityID.String()},
		{segment + "/leaderboard" + query, `"my_rank":`},
		{activity + "/segment-efforts", segmentID},
		{activity + "/merged-stream", `"heart_rate"`},
		{activity + "/similar", old.ActivityID.String()},
		{activity + "/laps", `"laps":[`},
		{activity + "/manage", `"provider":"garmin"`},
		{activity + "/export/options", `"gps":true`},
		{activity + "/export.gpx", "<trkpt"},
	} {
		requireBody(req.path, req.want)
		env.anon().do(http.MethodGet, req.path, nil, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound)
	}
	performance := readPerformanceMap(t, bob, "level=2&member="+alice.name)
	if performance.WithTrack < 2 || !slices.ContainsFunc(performance.GeoJSON.Features, func(row performanceMapFeature) bool {
		return row.Properties.Power != nil && *row.Properties.Power == 225
	}) {
		t.Fatalf("community performance map did not include recorded Garmin watts: %d tracks", performance.WithTrack)
	}
	garminPower := readRecordBoard(t, bob, "/api/records/leaderboards/ride/power/duration/300")
	if !slices.ContainsFunc(garminPower, func(row rankedRecord) bool {
		return row.Result != nil && row.Result.Username == alice.name && row.Result.AchievedValue == 225
	}) {
		t.Fatal("community power board omitted the Garmin recording")
	}

	bob.do(http.MethodGet, activity+"/map.png", nil, http.StatusOK)
	env.anon().do(http.MethodGet, activity+"/map.png", nil, http.StatusUnauthorized, http.StatusNotFound)
	if payload := bob.do(http.MethodGet, "/api/activities/feed?member="+bob.name, nil, http.StatusOK); bytes.Contains(payload, []byte(old.ActivityID.String())) {
		t.Fatal("switching to Bob still returned Alice's activity")
	}
	bob.do(http.MethodGet, "/api/stats?member=unknown-test-rider", nil, http.StatusNotFound)
	aid := old.ActivityID.String()
	getActivity := bob.post("/cairn.v1.ActivityService/GetActivity", map[string]any{"id": aid}, http.StatusOK)
	row := getActivity["activity"].(map[string]any)
	sources := row["sources"].([]any)
	source := sources[0].(map[string]any)
	if row["userId"] != alice.userID.String() || source["provider"] != "garmin" ||
		source["externalAccountId"] != nil || source["rawBlobId"] != nil || source["externalId"] != nil {
		t.Fatalf("incorrect activity or unredacted source: %+v", row)
	}
	env.anon().rpc("/cairn.v1.ActivityService/GetActivity", map[string]any{"id": aid}, http.StatusUnauthorized)
	bob.rpc("/cairn.v1.ActivityService/GetActivityStream", map[string]any{"activitySourceId": old.ID.String()}, http.StatusOK)
	bob.rpc("/cairn.v1.ActivityService/ListBestEfforts", map[string]any{"activityId": aid}, http.StatusOK)
	bob.rpc("/cairn.v1.ActivityService/ListSimilarActivities", map[string]any{"activityId": aid}, http.StatusOK)
	bob.rpc("/cairn.v1.ActivityService/GetMergedStream", map[string]any{"activityId": aid}, http.StatusOK)
	bob.rpc("/cairn.v1.StatsService/GetStats", map[string]any{"member": alice.name}, http.StatusOK)
	bob.rpc("/cairn.v1.StatsService/GetHeatmap", map[string]any{"member": alice.name}, http.StatusOK)
	gear := bob.post(gearRPC+"ListGear", map[string]any{"member": alice.name}, http.StatusOK)["gear"].([]any)
	if len(gear) != 1 || gear[0].(map[string]any)["id"] != gearID {
		t.Fatalf("wrong member gear: %v", gear)
	}
	bob.rpc(gearRPC+"DeleteGear", map[string]any{"id": gearID}, http.StatusNotFound, http.StatusForbidden)
	bob.rpc(gearRPC+"GetGear", map[string]any{"id": gearID, "member": alice.name}, http.StatusOK)
	bob.rpc("/cairn.v1.ActivityService/UpdateActivity", map[string]any{
		"id": aid, "field_mask": []string{"title"}, "title": "unauthorized change",
	}, http.StatusNotFound, http.StatusForbidden)
	bob.rpc("/cairn.v1.ActivityService/DeleteActivity", map[string]any{"id": aid}, http.StatusNotFound, http.StatusForbidden)
	bob.do(http.MethodPost, activity+"/recompute", nil, http.StatusNotFound)
	bob.do(http.MethodPost, activity+"/rescan-segments", nil, http.StatusNotFound)
	bob.do(http.MethodGet, activity+"/sources/"+old.ID.String()+"/parsed", nil, http.StatusNotFound)
	bob.do(http.MethodDelete, "/api/athlete/metrics/"+metricID, nil, http.StatusOK)
	if body := alice.do(http.MethodGet, "/api/athlete/metrics", nil, http.StatusOK); !bytes.Contains(body, []byte(metricID)) {
		t.Fatal("another rider deleted an owner-only metric")
	}
	requireBody("/api/activities/feed"+query, "Garmin historical ride")
	connection := requireBody("/api/connections/garmin"+query, `"connected":false`)
	if bytes.Contains(connection, []byte("fixture-alice")) {
		t.Fatal("Garmin credentials leaked through the member selector")
	}

	analysis := func(c *itestClient, path string) float64 {
		t.Helper()
		var data struct{ TSS []float64 }
		if err := json.Unmarshal(c.do(http.MethodGet, path, nil, http.StatusOK), &data); err != nil {
			t.Fatal(err)
		}
		var peak float64
		for _, v := range data.TSS {
			peak = max(peak, v)
		}
		return peak
	}
	if peak := analysis(bob, "/api/analysis"+query+"&days=30"); peak <= 0 {
		t.Fatal("the recent Garmin ride did not contribute to Alice's visible training load")
	}
	admin := env.user("garmin-admin", domain.UserRoleAdmin)
	admin.post("/api/admin/activities/"+recent.ActivityID.String()+"/hide", map[string]any{"hidden": true}, http.StatusOK)
	if analysis(bob, "/api/analysis"+query+"&days=30") != 0 || analysis(alice, "/api/analysis?days=30") <= 0 {
		t.Fatal("administratively hidden ride contributed to the member's training load")
	}
	if body := bob.do(http.MethodGet, "/api/activities/feed"+query, nil, http.StatusOK); bytes.Contains(body, []byte(recent.ActivityID.String())) {
		t.Fatal("administratively hidden ride appeared on the member's activity list")
	}
}
