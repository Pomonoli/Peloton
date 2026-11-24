//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
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
	startITConsumers(t, env, startFollowUpConsumer, startSegmentDiscoveryConsumer)
}

func startITConsumers(t *testing.T, env *itestEnv, consumers ...func(context.Context, *App, *slog.Logger) (port.Subscription, error)) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	for _, start := range consumers {
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
	SpeedKmh  float64 `json:"average_speed_kmh"`
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

type boardRow struct {
	EffortCount        int       `json:"effort_count"`
	AllEffortsSpeedKmh float64   `json:"all_efforts_speed_kmh"`
	Rank               int       `json:"rank"`
	UserID             string    `json:"user_id"`
	ActivityID         string    `json:"activity_id"`
	Name               string    `json:"display_name"`
	Elapsed            float64   `json:"elapsed_s"`
	SpeedKmh           float64   `json:"average_speed_kmh"`
	StartTime          time.Time `json:"start_time"`
}

func segmentBoard(t *testing.T, c *itestClient, segmentID string) []boardRow {
	t.Helper()
	var result struct {
		Entries []boardRow
	}
	if err := json.Unmarshal(c.do(http.MethodGet, "/api/segments/"+segmentID+"/leaderboard", nil, http.StatusOK), &result); err != nil {
		t.Fatal(err)
	}
	return result.Entries
}

type catalogRow struct {
	ID            string  `json:"id"`
	MemberCount   int     `json:"member_count"`
	TotalAttempts int     `json:"total_attempts"`
	Winner        string  `json:"local_kom_holder"`
	Elapsed       float64 `json:"best_elapsed_s"`
	SpeedKmh      float64 `json:"average_speed_kmh"`
}

func catalog(t *testing.T, c *itestClient, filter string) []catalogRow {
	t.Helper()
	var response struct {
		Segments []catalogRow
	}
	if err := json.Unmarshal(c.do(http.MethodGet, "/api/segments/catalog"+filter, nil, http.StatusOK), &response); err != nil {
		t.Fatal(err)
	}
	return response.Segments
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
		math.Abs(got[0].SpeedKmh-seg.DistanceM/got[0].Elapsed*3.6) > 0.000001 {
		t.Fatalf("unexpected historical effort: %+v", got)
	}
	segmentID := got[0].SegmentID
	// Bob has never ridden this segment and neither rider follows the other.
	bob.do(http.MethodGet, "/api/segments/"+segmentID, nil, http.StatusOK)
	if board := segmentBoard(t, bob, segmentID); len(board) != 1 || board[0].UserID != alice.userID.String() {
		t.Fatalf("new member did not see the shared leaderboard: %+v", board)
	}
	if rows := catalog(t, bob, ""); len(rows) != 1 || rows[0].ID != segmentID || rows[0].MemberCount != 1 {
		t.Fatalf("unexpected initial community catalog: %+v", rows)
	}
	if rows := catalog(t, bob, "?mine=true"); len(rows) != 0 {
		t.Fatalf("My Segments included a ride Bob had not completed: %+v", rows)
	}
	if peer := activitySegmentEfforts(t, bob, activity); len(peer) != 1 || peer[0] != got[0] {
		t.Fatalf("member's activity effort missing or different: %+v", peer)
	}
	anon := env.anon()
	for _, path := range []string{"/api/segments/catalog", "/api/segments/" + segmentID, "/api/segments/" + segmentID + "/leaderboard", "/api/activities/" + activity + "/segment-efforts"} {
		anon.do(http.MethodGet, path, nil, http.StatusUnauthorized)
	}
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
	setITSegmentVisibility(bob, bobActivity, "PRIVATE")
	if privateBoard := segmentBoard(t, alice, segmentID); len(privateBoard) != 1 || privateBoard[0].UserID != alice.userID.String() {
		t.Fatalf("private effort appeared in shared standings: %+v", privateBoard)
	}
	setITSegmentVisibility(bob, bobActivity, "PUBLIC")
	board := segmentBoard(t, alice, segmentID)
	if len(board) != 2 || board[0].UserID != bob.userID.String() || board[0].Rank != 1 || board[1].Rank != 2 ||
		board[0].StartTime.IsZero() || math.Abs(board[0].SpeedKmh-seg.DistanceM/100*3.6) > 0.000001 {
		t.Fatalf("expected two local standings, got %+v", board)
	}
	if got := activitySegmentEfforts(t, alice, activity); len(got) != 1 || got[0].KOM != 2 {
		t.Fatalf("activity did not display the viewer's Local KOM position: %+v", got)
	}
	all, mine := catalog(t, alice, ""), catalog(t, alice, "?mine=true")
	if len(all) != 1 || len(mine) != 1 || all[0] != mine[0] || all[0].ID != segmentID ||
		all[0].MemberCount != 2 || all[0].Winner != board[0].Name || all[0].Elapsed != board[0].Elapsed || all[0].SpeedKmh != board[0].SpeedKmh {
		t.Fatalf("catalog and My Segments should agree with the shared standings: all=%+v mine=%+v", all, mine)
	}
	slow := alice.upload("older-ride.gpx", gpxRideAtInterval(time.Date(2019, 4, 6, 12, 0, 0, 0, time.UTC), 2*time.Second))
	slowID := str(slow, "activity_id")
	if efforts := waitForSegmentEfforts(t, alice, slowID, 1); len(efforts) != 1 || efforts[0].Elapsed != 400 || efforts[0].PR {
		t.Fatalf("a slower historical import replaced the PR: %+v", efforts)
	}

	if board := segmentBoard(t, bob, segmentID); len(board) != 2 || board[0].EffortCount != 1 ||
		board[1].EffortCount != 2 || math.Abs(board[1].AllEffortsSpeedKmh-seg.DistanceM/300*3.6) > 0.000001 {
		t.Fatalf("leaderboard rivalry counts and speeds: %+v", board)
	}
	var period struct{ Entries []boardRow }
	query := "/api/segments/" + segmentID + "/leaderboard?from=2020-01-01T00:00:00Z&to=2022-01-01T00:00:00Z"
	if err := json.Unmarshal(alice.do(http.MethodGet, query, nil, http.StatusOK), &period); err != nil {
		t.Fatal(err)
	}
	if len(period.Entries) != 1 || period.Entries[0].UserID != bob.userID.String() || period.Entries[0].EffortCount != 1 {
		t.Fatalf("date window: %+v", period.Entries)
	}
	var history struct {
		Efforts []struct {
			ActivityID string `json:"activity_id"`
		}
	}
	query = "/api/segments/" + segmentID + "/efforts?valid_only=true&user_id=" + alice.userID.String() + "&from=2020-01-01T00:00:00Z"
	if err := json.Unmarshal(bob.do(http.MethodGet, query, nil, http.StatusOK), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Efforts) != 1 || history.Efforts[0].ActivityID != activity {
		t.Fatalf("visible rider history: %+v", history.Efforts)
	}
	sid, err := domain.ParseUUID[domain.SourceID](str(upload, "source_id"))
	if err != nil {
		t.Fatal(err)
	}
	if err := env.app.Segments.DeleteEffortsForSource(t.Context(), sid); err != nil {
		t.Fatal(err)
	}
	alice.do(http.MethodPost, "/api/activities/"+activity+"/rescan-segments", nil, http.StatusAccepted)
	if efforts := waitForSegmentEfforts(t, alice, activity, 1); len(efforts) != 1 || efforts[0].Elapsed != got[0].Elapsed || efforts[0].KOM != 2 || !efforts[0].PR {
		t.Fatalf("rescan did not restore the same cached effort: %+v", efforts)
	}
	if provider.calls.Load() != before {
		t.Fatal("repeat imports or rescans made unnecessary provider requests")
	}
	if board := segmentBoard(t, alice, segmentID); len(board) != 2 || board[1].Elapsed != 200 {
		t.Fatalf("repeat efforts must not create new competitors: %+v", board)
	}
	alice.do(http.MethodGet, "/api/segments/"+segmentID, nil, http.StatusOK)
	alice.do(http.MethodGet, "/api/segments", nil, http.StatusOK)
}

func TestAPI_SegmentCatalogSorting(t *testing.T) {
	env := newITestEnv(t)
	ctx := t.Context()
	riders := []*itestClient{
		env.user("alex", domain.UserRoleUser),
		env.user("zoe", domain.UserRoleUser),
		env.user("mira", domain.UserRoleUser),
	}
	if err := env.app.Users.UpdateProfile(ctx, riders[2].userID, port.ProfileUpdate{
		DisplayName: " ", Locale: "en", Timezone: "UTC", Units: "metric",
	}); err != nil {
		t.Fatal(err)
	}

	start := time.Date(2022, 9, 21, 12, 0, 0, 0, time.UTC)
	activities := make([]domain.ActivityID, len(riders))
	sources := make([]domain.SourceID, len(riders))
	for i, rider := range riders {
		upload := rider.upload("catalog-ride.gpx", gpxRide(start))
		var err error
		activities[i], err = domain.ParseUUID[domain.ActivityID](str(upload, "activity_id"))
		if err != nil {
			t.Fatal(err)
		}
		sources[i], err = domain.ParseUUID[domain.SourceID](str(upload, "source_id"))
		if err != nil {
			t.Fatal(err)
		}
	}

	names := make(map[string]string)
	provider := "test-map"
	for i, fixture := range []struct {
		name     string
		distance float64
		times    []float64 // by rider, with zero for no effort
	}{
		{"Dune", 400, []float64{7500, 0, 7200}},
		{"Brook", 1200, []float64{120, 145, 0}},
		{"alpine", 90, []float64{0, 9, 0}},
		{"Canal", 200, []float64{10.25, 0, 0}},
	} {
		externalID := fmt.Sprint(i + 1)
		seg := domain.Segment{
			ID: domain.SegmentID(uuid.New()), Source: domain.SegmentSourceStravaCatalog, ExternalID: &externalID,
			CatalogProvider: &provider, Scope: domain.SegmentScopeInstance, Name: fixture.name,
			ActivityType: domain.ActivityTypeRide, DistanceM: fixture.distance,
			Polyline: "gt{}G_hzo@o}@_q@", PolylinePrecision: 5,
		}
		if err := env.app.Segments.SaveSegment(ctx, seg); err != nil {
			t.Fatal(err)
		}
		names[seg.ID.String()] = seg.Name
		for j, elapsed := range fixture.times {
			if elapsed == 0 {
				continue
			}
			if err := env.app.Segments.SaveSegmentEffort(ctx, domain.SegmentEffort{
				ID: domain.SegmentEffortID(uuid.New()), SegmentID: seg.ID, ActivityID: activities[j],
				ActivitySourceID: sources[j], UserID: riders[j].userID, StartTime: start,
				ElapsedS: elapsed, MovingS: elapsed, EndOffset: 1,
			}); err != nil {
				t.Fatal(err)
			}
			if fixture.name == "Dune" && j == 0 {
				if err := env.app.Segments.SaveSegmentEffort(ctx, domain.SegmentEffort{
					ID: domain.SegmentEffortID(uuid.New()), SegmentID: seg.ID, ActivityID: activities[j],
					ActivitySourceID: sources[j], UserID: riders[j].userID, StartTime: start.Add(time.Hour),
					ElapsedS: elapsed + 10, MovingS: elapsed + 10, StartOffset: 2, EndOffset: 3,
				}); err != nil {
					t.Fatal(err)
				}
			}

		}
	}

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"", []string{"Dune", "Brook", "Canal", "alpine"}},
		{"mine=true", []string{"Dune", "Brook", "Canal"}},
		{"sort=name&direction=desc", []string{"Dune", "Canal", "Brook", "alpine"}},
		{"sort=distance_m", []string{"alpine", "Canal", "Dune", "Brook"}},
		{"sort=distance_m&direction=desc", []string{"Brook", "Dune", "Canal", "alpine"}},
		{"sort=best_elapsed_s", []string{"alpine", "Canal", "Brook", "Dune"}},
		{"sort=best_elapsed_s&direction=desc", []string{"Dune", "Brook", "Canal", "alpine"}},
		{"sort=average_speed_kmh", []string{"Dune", "alpine", "Brook", "Canal"}},
		{"sort=average_speed_kmh&direction=desc", []string{"Canal", "alpine", "Brook", "Dune"}},
		{"sort=local_kom_holder", []string{"Brook", "Canal", "alpine", "Dune"}},
		{"sort=local_kom_holder&direction=desc", []string{"alpine", "Brook", "Canal", "Dune"}},
		{"sort=member_count", []string{"alpine", "Canal", "Brook", "Dune"}},
		{"sort=member_count&direction=desc", []string{"Brook", "Dune", "alpine", "Canal"}},
		{"mine=true&sort=member_count&direction=desc", []string{"Brook", "Dune", "Canal"}},
		{"mine=true&sort=average_speed_kmh&direction=desc", []string{"Canal", "Brook", "Dune"}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			const limit = 2
			for offset := 0; offset < len(tc.want); offset += limit {
				path := fmt.Sprintf("/api/segments/catalog?limit=%d&offset=%d&%s", limit, offset, tc.query)
				var page struct {
					Segments []catalogRow
					HasMore  bool `json:"has_more"`
				}
				if err := json.Unmarshal(riders[0].do(http.MethodGet, path, nil, http.StatusOK), &page); err != nil {
					t.Fatal(err)
				}
				got := make([]string, 0, len(page.Segments))
				for _, row := range page.Segments {
					got = append(got, names[row.ID])
					attempts := map[string]int{"Dune": 3, "Brook": 2, "Canal": 1, "alpine": 1}
					if count := attempts[names[row.ID]]; row.TotalAttempts != count {
						t.Errorf("%s: total attempts = %d, want %d", names[row.ID], row.TotalAttempts, count)
					}
				}
				want := tc.want[offset:min(offset+limit, len(tc.want))]
				if !slices.Equal(got, want) || page.HasMore != (offset+limit < len(tc.want)) {
					t.Fatalf("offset %d: got %v (has_more %t), want %v", offset, got, page.HasMore, want)
				}
			}
		})
	}
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
