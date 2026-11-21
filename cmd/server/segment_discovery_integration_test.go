//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type testDiscoveryProvider struct {
	calls   atomic.Int32
	first   chan struct{}
	release chan struct{}
	page    port.SegmentDiscoveryPage
}

func (p *testDiscoveryProvider) Name() string { return "test-map" }
func (p *testDiscoveryProvider) Discover(ctx context.Context, box port.BoundingBox) (port.SegmentDiscoveryPage, error) {
	if p.calls.Add(1) == 1 {
		close(p.first)
		select {
		case <-ctx.Done():
			return port.SegmentDiscoveryPage{}, ctx.Err()
		case <-p.release:
			return port.SegmentDiscoveryPage{}, errors.New("temporary provider error")
		}
	}
	if box.MinLat > 47.005 || box.MaxLat < 47.015 {
		return port.SegmentDiscoveryPage{}, nil
	}
	return p.page, nil
}

func newITDiscoveryEnv(t *testing.T) *itestEnv {
	t.Helper()
	if os.Getenv("CAIRN_TEST_DATABASE_URL") == "" {
		t.Skip("CAIRN_TEST_DATABASE_URL is unset")
	}
	nats, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true,
		StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go nats.Start()
	t.Cleanup(nats.Shutdown)
	if !nats.ReadyForConnections(10 * time.Second) {
		t.Fatal("NATS did not start")
	}
	return newITestEnvWithOverrides(t, map[string]string{
		"CAIRN_NATS_URL":                  nats.ClientURL(),
		"CAIRN_SEGMENT_DISCOVERY_ENABLED": "true",
	})
}

func startITDiscovery(t *testing.T, env *itestEnv) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	for _, start := range []func(context.Context, *App, *slog.Logger) (port.Subscription, error){startFollowUpConsumer, startSegmentDiscoveryConsumer} {
		sub, err := start(ctx, env.app, testLogger())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sub.Close(context.Background()) })
	}
}

type discoveredEffort struct {
	SegmentID string  `json:"segment_id"`
	Elapsed   float64 `json:"elapsed_s"`
	Speed     float64 `json:"avg_speed_mps"`
	PR        bool    `json:"is_personal_record"`
	KOM       int     `json:"local_kom_position"`
}

func activitySegmentEfforts(t *testing.T, c *itestClient, activityID string) []discoveredEffort {
	t.Helper()
	var result struct{ Efforts []discoveredEffort }
	if err := json.Unmarshal(c.do(http.MethodGet, "/api/activities/"+activityID+"/segment-efforts", nil, http.StatusOK), &result); err != nil {
		t.Fatal(err)
	}
	return result.Efforts
}

func waitForSegmentEfforts(t *testing.T, c *itestClient, activityID string, count int) []discoveredEffort {
	t.Helper()
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); {
		efforts := activitySegmentEfforts(t, c, activityID)
		if len(efforts) >= count {
			return efforts
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("discovery did not produce the expected number of efforts")
	return nil
}

func setITSegmentVisibility(c *itestClient, activityID, privacy string) {
	c.rpc("/cairn.v1.ActivityService/UpdateActivity", map[string]any{
		"id": activityID, "field_mask": []string{"privacy"}, "privacy": "PRIVACY_" + privacy,
	}, http.StatusOK)
}

func segmentBoard(t *testing.T, c *itestClient, segmentID string) []domain.LeaderboardEntry {
	t.Helper()
	var result struct {
		Entries []struct {
			Rank       int     `json:"rank"`
			UserID     string  `json:"user_id"`
			ActivityID string  `json:"activity_id"`
			Elapsed    float64 `json:"elapsed_s"`
		}
	}
	if err := json.Unmarshal(c.do(http.MethodGet, "/api/segments/"+segmentID+"/leaderboard", nil, http.StatusOK), &result); err != nil {
		t.Fatal(err)
	}
	var board []domain.LeaderboardEntry
	for _, row := range result.Entries {
		u, _ := domain.ParseUUID[domain.UserID](row.UserID)
		a, _ := domain.ParseUUID[domain.ActivityID](row.ActivityID)
		board = append(board, domain.LeaderboardEntry{Rank: row.Rank, UserID: u, ActivityID: a, ElapsedS: row.Elapsed})
	}
	return board
}

func TestAPI_DiscoveredSegments(t *testing.T) {
	env := newITDiscoveryEnv(t)
	id, providerName := "123456789", "test-map"
	seg := domain.Segment{
		Source: domain.SegmentSourceStravaCatalog, ExternalID: &id, CatalogProvider: &providerName,
		Scope: domain.SegmentScopeInstance, Name: "Integration test segment", ActivityType: domain.ActivityTypeRide,
		Polyline: "gt{}G_hzo@o}@_q@", PolylinePrecision: 5,
	}
	points, err := seg.DecodedPolyline()
	if err != nil {
		t.Fatal(err)
	}
	seg.DistanceM = domain.PolylineDistanceM(points)
	provider := &testDiscoveryProvider{
		first: make(chan struct{}), release: make(chan struct{}),
		page: port.SegmentDiscoveryPage{Segments: []domain.Segment{seg, seg}},
	}
	env.app.DiscoverSegments.Provider = provider
	startITDiscovery(t, env)
	alice := env.user("segment-alice", domain.UserRoleUser)
	bob := env.user("segment-bob", domain.UserRoleUser)
	bob.do(http.MethodPut, "/api/profile/visibility", map[string]any{
		"policy": map[string][]string{"public": {"summary", "segments"}},
	}, http.StatusOK)

	gpx := gpxRide(time.Date(2022, 9, 21, 12, 0, 0, 0, time.UTC))
	upload := alice.upload("test-ride.gpx", gpx)
	activity := str(upload, "activity_id")
	select {
	case <-provider.first:
	case <-time.After(10 * time.Second):
		t.Fatal("the saved upload did not trigger discovery")
	}
	if len(activitySegmentEfforts(t, alice, activity)) != 0 {
		t.Fatal("an effort appeared before the provider returned its definition")
	}
	close(provider.release)
	got := waitForSegmentEfforts(t, alice, activity, 1)
	if len(got) != 1 || got[0].Elapsed != 200 || !got[0].PR || got[0].KOM != 1 ||
		math.Abs(got[0].Speed-seg.DistanceM/got[0].Elapsed) > 0.000001 {
		t.Fatalf("unexpected historical effort: %+v", got)
	}
	segmentID := got[0].SegmentID
	bob.do(http.MethodGet, "/api/segments/"+segmentID+"/leaderboard", nil, http.StatusNotFound)
	bob.do(http.MethodPost, "/api/activities/"+activity+"/rescan-segments", nil, http.StatusNotFound)

	repeated := alice.upload("duplicate.gpx", gpx)
	if str(repeated, "activity_id") != activity || str(repeated, "source_id") != str(upload, "source_id") {
		t.Fatal("repeated upload did not reuse the original activity and source")
	}
	before := provider.calls.Load()
	bobActivity := str(bob.upload("same-route.gpx", gpxRideAtInterval(time.Date(2021, 4, 6, 12, 0, 0, 0, time.UTC), 500*time.Millisecond)), "activity_id")
	bobEfforts := waitForSegmentEfforts(t, bob, bobActivity, 1)
	if len(bobEfforts) != 1 || bobEfforts[0].Elapsed != 100 || bobEfforts[0].SegmentID != segmentID || provider.calls.Load() != before {
		t.Fatal("a second rider did not reuse the cached segment")
	}
	setITSegmentVisibility(bob, bobActivity, "PUBLIC")
	board := segmentBoard(t, alice, segmentID)
	if len(board) != 2 || board[0].UserID != bob.userID || board[0].Rank != 1 || board[1].Rank != 2 {
		t.Fatalf("expected two local standings, got %+v", board)
	}
	if got := activitySegmentEfforts(t, alice, activity); len(got) != 1 || got[0].KOM != 2 {
		t.Fatalf("activity did not display the viewer's Local KOM position: %+v", got)
	}
	setITSegmentVisibility(bob, bobActivity, "PRIVATE")
	if board := segmentBoard(t, alice, segmentID); len(board) != 1 || board[0].UserID != alice.userID {
		t.Fatal("private effort visible on another rider's leaderboard")
	}
	slow := alice.upload("older-ride.gpx", gpxRideAtInterval(time.Date(2019, 4, 6, 12, 0, 0, 0, time.UTC), 2*time.Second))
	slowID := str(slow, "activity_id")
	if efforts := waitForSegmentEfforts(t, alice, slowID, 1); len(efforts) != 1 || efforts[0].Elapsed != 400 || efforts[0].PR {
		t.Fatalf("a slower historical import replaced the PR: %+v", efforts)
	}

	sid, err := domain.ParseUUID[domain.SourceID](str(upload, "source_id"))
	if err != nil {
		t.Fatal(err)
	}
	if err := env.app.Segments.DeleteEffortsForSource(t.Context(), sid); err != nil {
		t.Fatal(err)
	}
	alice.do(http.MethodPost, "/api/activities/"+activity+"/rescan-segments", nil, http.StatusAccepted)
	if efforts := waitForSegmentEfforts(t, alice, activity, 1); len(efforts) != 1 || efforts[0] != got[0] {
		t.Fatalf("rescan did not restore the same cached effort: %+v", efforts)
	}
	if provider.calls.Load() != before {
		t.Fatal("repeat imports or rescans made unnecessary provider requests")
	}
	alice.do(http.MethodGet, "/api/segments/"+segmentID, nil, http.StatusOK)
	alice.do(http.MethodGet, "/api/segments", nil, http.StatusOK)
}

func TestAPI_StravaDiscoveryRecordedGPX(t *testing.T) {
	path := os.Getenv("PELOTON_TEST_RECORDED_GPX")
	if path == "" {
		t.Skip("set PELOTON_TEST_RECORDED_GPX to the archived Brussels-area GPX from the discovery research")
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	// The exported GPX has no sport tag. Supply its known cycling type
	// while preserving every original point and timestamp.
	tag := []byte("<trkseg>")
	if !bytes.Contains(raw, tag) {
		t.Fatal("recorded GPX does not contain the expected track segment")
	}
	raw = bytes.Replace(raw, tag, []byte("<type>cycling</type><trkseg>"), 1)
	env := newITDiscoveryEnv(t)
	startITDiscovery(t, env)
	rider := env.user("recorded-ride", domain.UserRoleUser)
	upload := rider.upload("recorded-ride.gpx", raw)
	efforts := waitForSegmentEfforts(t, rider, str(upload, "activity_id"), 3)
	want := map[string]float64{"40628454": 271, "35700572": 201, "29325646": 367}
	for _, e := range efforts {
		var result struct {
			Segment struct {
				ExternalID string `json:"external_id"`
			}
		}
		if err := json.Unmarshal(rider.do(http.MethodGet, "/api/segments/"+e.SegmentID, nil, http.StatusOK), &result); err != nil {
			t.Fatal(err)
		}
		id := result.Segment.ExternalID
		if seconds, ok := want[id]; ok {
			if e.Elapsed != seconds || !e.PR || e.KOM != 1 {
				t.Errorf("Strava segment %s: expected %g s, PR and first place, got %+v", id, seconds, e)
			}
			delete(want, id)
		}
	}
	if len(want) != 0 {
		t.Fatalf("live provider scan missed expected recorded efforts: %v", want)
	}
}
