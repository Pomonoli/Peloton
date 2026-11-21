//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/export"
	"github.com/opencairn-org/cairn-core/internal/port"
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
	startITDiscovery(t, env)

	alice := env.user("garmin-alice", domain.UserRoleUser)
	bob := env.user("garmin-bob", domain.UserRoleUser)
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
