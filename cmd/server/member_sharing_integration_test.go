//go:build integration

package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/db"
	"github.com/opencairn-org/cairn-core/internal/domain"
)

func TestAPI_MemberSharingUpgrade(t *testing.T) {
	ctx := t.Context()
	env := newITDiscoveryEnv(t)
	if result, err := db.MigrateDown(ctx, env.pool); err != nil || result.Source.Version != 88 {
		t.Fatalf("prepare version 87 schema: %v, %v", result, err)
	}
	startITConsumers(t, env, startFollowUpConsumer)

	alice := env.user("legacy-alice", domain.UserRoleUser)
	bob := env.user("legacy-bob", domain.UserRoleUser)
	admin := env.user("legacy-admin", domain.UserRoleAdmin)
	start := time.Now().UTC().AddDate(0, 0, -10).Truncate(time.Hour)
	upload := func(c *itestClient, day, watts int) (id, source string) {
		t.Helper()
		ride := c.upload("historical.gpx", performanceGPX(start.AddDate(0, 0, day), time.Second, 145, watts))
		waitForPersonalPower(t, c, float64(watts))
		return str(ride, "activity_id"), str(ride, "source_id")
	}
	aliceID, aliceSource := upload(alice, 0, 220)
	bobID, bobSource := upload(bob, 1, 265)
	hiddenID, _ := upload(bob, 2, 320)
	restrictedID, _ := upload(bob, 3, 410)
	admin.post("/api/admin/activities/"+hiddenID+"/hide", map[string]bool{"hidden": true}, http.StatusOK)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := env.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE users SET profile_is_public = false WHERE id IN ($1, $2)", alice.userID.UUID(), bob.userID.UUID())
	exec("INSERT INTO user_settings(user_id, default_activity_privacy) VALUES ($1, 'private'), ($2, 'followers') ON CONFLICT (user_id) DO UPDATE SET default_activity_privacy = EXCLUDED.default_activity_privacy", alice.userID.UUID(), bob.userID.UUID())
	exec(`INSERT INTO user_visibility_defaults(user_id, policy)
		VALUES ($1, '{}'), ($2, '{"public":["summary"],"followers":[],"link":[]}')
		ON CONFLICT (user_id) DO UPDATE SET policy = EXCLUDED.policy`, alice.userID.UUID(), bob.userID.UUID())
	exec(`UPDATE activities SET privacy = 'private', visibility_override = '{}' WHERE user_id = $1`, alice.userID.UUID())
	exec(`UPDATE activities SET privacy = 'followers', visibility_override = '{"public":[]}' WHERE user_id = $1`, bob.userID.UUID())
	exec("UPDATE activities SET provider_restricted = true WHERE id = $1", restrictedID)

	segment := domain.Segment{
		ID: domain.SegmentID(uuid.New()), Name: "Historical Canal", Source: domain.SegmentSourceStravaCatalog,
		ExternalID: new("672991"), CatalogProvider: new("test-map"), Scope: domain.SegmentScopeInstance,
		ActivityType: domain.ActivityTypeRide, Polyline: "gt{}G_hzo@o}@_q@", PolylinePrecision: 5, DistanceM: 1200,
	}
	if err := env.app.Segments.SaveSegment(ctx, segment); err != nil {
		t.Fatal(err)
	}
	for _, ride := range []struct {
		member     domain.UserID
		id, source string
		elapsed    float64
	}{{alice.userID, aliceID, aliceSource, 110}, {bob.userID, bobID, bobSource, 105}} {
		if err := env.app.Segments.SaveSegmentEffort(ctx, domain.SegmentEffort{
			ID: domain.SegmentEffortID(uuid.New()), SegmentID: segment.ID, UserID: ride.member,
			ActivityID: domain.ActivityID(uuid.MustParse(ride.id)), ActivitySourceID: domain.SourceID(uuid.MustParse(ride.source)),
			ElapsedS: ride.elapsed, MovingS: ride.elapsed, StartTime: start, StartOffset: 0, EndOffset: 100,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if applied, err := db.MigrateUp(ctx, env.pool); err != nil || len(applied) != 1 || applied[0].Source.Version != 88 {
		t.Fatalf("upgrade to member sharing: %v, %v", applied, err)
	}
	var profiles, activities, sources, settings, policies int
	err := env.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM users WHERE id IN ($1, $2) AND profile_is_public),
		(SELECT count(*) FROM activities WHERE user_id IN ($1, $2) AND privacy = 'public' AND visibility_override IS NULL),
		(SELECT count(*) FROM activity_sources WHERE (id = $3 AND activity_id = $4) OR (id = $5 AND activity_id = $6)),
		(SELECT count(*) FROM user_settings WHERE user_id IN ($1, $2) AND default_activity_privacy = 'public'),
		(SELECT count(*) FROM user_visibility_defaults WHERE user_id IN ($1, $2))`,
		alice.userID.UUID(), bob.userID.UUID(), aliceSource, aliceID, bobSource, bobID).
		Scan(&profiles, &activities, &sources, &settings, &policies)
	if err != nil || profiles != 2 || activities != 4 || sources != 2 || settings != 2 || policies != 0 {
		t.Fatalf("migration: profiles=%d rides=%d sources=%d defaults=%d legacy policies=%d, err=%v",
			profiles, activities, sources, settings, policies, err)
	}

	anon := env.anon()
	for _, entry := range []struct {
		viewer, rider *itestClient
		activityID    string
	}{{alice, bob, bobID}, {bob, alice, aliceID}} {
		q := "?member=" + entry.rider.name
		for _, path := range []string{
			"/api/profiles/" + entry.rider.name,
			"/api/overview" + q,
			"/api/activities/feed" + q,
			"/api/stats" + q,
			"/api/activities/heatmap" + q,
			"/api/records" + q,
			"/api/riders",
		} {
			body := string(entry.viewer.do(http.MethodGet, path, nil, http.StatusOK))
			if !strings.Contains(body, entry.activityID) || entry.viewer == alice &&
				(strings.Contains(body, hiddenID) || strings.Contains(body, restrictedID)) {
				t.Errorf("%s: wrong historical activities: %s", path, body)
			}
			anon.do(http.MethodGet, path, nil, http.StatusUnauthorized)
		}

		analysisPath := "/api/analysis" + q
		var analysis struct {
			Coverage struct {
				Imported         int
				SensorEstimate   int `json:"sensor_estimate"`
				DurationEstimate int `json:"duration_estimate"`
				Missing          int
			}
		}
		if err := json.Unmarshal(entry.viewer.do(http.MethodGet, analysisPath, nil, http.StatusOK), &analysis); err != nil {
			t.Fatal(err)
		}
		// Analysis reports the number of eligible activities contributing to its load series.
		if coverage := analysis.Coverage; coverage.Imported+coverage.SensorEstimate+coverage.DurationEstimate+coverage.Missing != 1 {
			t.Fatalf("analysis did not include exactly one eligible ride: %+v", coverage)
		}
		anon.do(http.MethodGet, analysisPath, nil, http.StatusUnauthorized)
		if result := readPerformanceMap(t, entry.viewer, "member="+entry.rider.name); result.WithTrack != 1 || len(result.GeoJSON.Features) == 0 {
			t.Fatalf("missing historical performance heatmap: %+v", result)
		}
		anon.do(http.MethodGet, "/api/activities/heatmap/performance"+q, nil, http.StatusUnauthorized)

		const activityRPC = "/cairn.v1.ActivityService/"
		request := map[string]any{"id": entry.activityID}
		entry.viewer.rpc(activityRPC+"GetActivity", request, http.StatusOK)
		anon.rpc(activityRPC+"GetActivity", request, http.StatusUnauthorized)
		entry.viewer.rpc(activityRPC+"UpdateActivity", map[string]any{
			"id": entry.activityID, "field_mask": []string{"title"}, "title": "Another rider's edit",
		}, http.StatusNotFound)
	}
	overview := "/api/segments/catalog?member=" + bob.name + "&mine=true"
	items := catalog(t, alice, "?member="+bob.name+"&mine=true")
	if len(items) != 1 || items[0].ID != segment.ID.String() || items[0].MemberCount != 2 {
		t.Fatalf("historical segment catalog: %+v", items)
	}
	anon.do(http.MethodGet, overview, nil, http.StatusUnauthorized)
	if board := segmentBoard(t, alice, segment.ID.String()); len(board) != 2 || board[0].UserID != bob.userID.String() {
		t.Fatalf("historical segment leaderboard: %+v", board)
	}
	const recordsPath = "/api/records/leaderboards/ride/power/duration/300"
	if board := readRecordBoard(t, alice, recordsPath); len(board) != 2 || board[0].Result.ActivityID != bobID || board[1].Result.ActivityID != aliceID {
		t.Fatalf("historical record leaderboard: %+v", board)
	}
	anon.do(http.MethodGet, recordsPath, nil, http.StatusUnauthorized)
	comparison := readHeadToHead(t, alice, bob)
	if len(comparison.Segments) != 1 || comparison.Segments[0].You.ActivityID != aliceID ||
		comparison.Segments[0].Opponent.ActivityID != bobID || comparison.RecordScore.Comparable == 0 {
		t.Fatalf("historical Head-to-Head: %+v", comparison)
	}
	anon.do(http.MethodGet, "/api/head-to-head?member="+bob.name, nil, http.StatusUnauthorized)
	for _, id := range []string{hiddenID, restrictedID} {
		alice.rpc("/cairn.v1.ActivityService/GetActivity", map[string]any{"id": id}, http.StatusNotFound)
		bob.rpc("/cairn.v1.ActivityService/GetActivity", map[string]any{"id": id}, http.StatusOK)
	}
}
