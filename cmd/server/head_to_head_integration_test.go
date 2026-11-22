//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

type headToHeadResponse struct {
	Segments []struct {
		Name          string
		You, Opponent struct {
			ActivityID string `json:"activity_id"`
			Value      float64
		}
		Winner          string
		TimeDifference  float64 `json:"time_difference_s"`
		SpeedDifference float64 `json:"speed_difference_kmh"`
	}
	Records []struct {
		Metric        string
		Window        int `json:"window_value"`
		You, Opponent *struct {
			ActivityID string `json:"activity_id"`
			Value      float64
		}
		Winner string
	}
	SegmentScore struct{ Wins, Losses, Ties, Comparable int } `json:"segment_score"`
	RecordScore  struct{ Wins, Losses, Ties, Comparable int } `json:"record_score"`
}

func readHeadToHead(t *testing.T, viewer, opponent *itestClient) headToHeadResponse {
	t.Helper()
	var result headToHeadResponse
	body := viewer.do(http.MethodGet, "/api/head-to-head?member="+opponent.name, nil, http.StatusOK)
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("comparison: %v: %s", err, body)
	}
	return result
}

func TestAPI_HeadToHead(t *testing.T) {
	ctx := t.Context()
	env := newITDiscoveryEnv(t)
	startITConsumers(t, env, startFollowUpConsumer)
	alice := env.user("compare-alice", domain.UserRoleUser)
	bob := env.user("compare-bob", domain.UserRoleUser)
	start := time.Date(2021, 2, 1, 8, 0, 0, 0, time.UTC)
	type ride struct {
		activity domain.ActivityID
		source   domain.SourceID
		member   domain.UserID
	}
	upload := func(c *itestClient, day, watts int, privacy string) ride {
		t.Helper()
		raw := c.upload("record.gpx", performanceGPX(start.AddDate(0, 0, day), time.Second, 145, watts))
		a := str(raw, "activity_id")
		setITSegmentVisibility(c, a, privacy)
		waitForPersonalPower(t, c, float64(watts))
		return ride{domain.ActivityID(uuid.MustParse(a)), domain.SourceID(uuid.MustParse(str(raw, "source_id"))), c.userID}
	}
	a := upload(alice, 0, 260, "PRIVATE")
	b := upload(bob, 1, 240, "FOLLOWERS")
	private := upload(bob, 2, 390, "PRIVATE")
	const endpoint = "/api/head-to-head?member="
	env.anon().do(http.MethodGet, endpoint+bob.name, nil, http.StatusUnauthorized)
	alice.do(http.MethodGet, endpoint+alice.name, nil, http.StatusBadRequest)
	alice.do(http.MethodGet, endpoint+"unknown-rider", nil, http.StatusNotFound)
	if page := readHeadToHead(t, alice, bob); page.RecordScore.Comparable != 0 || len(page.Segments) != 0 {
		t.Fatalf("unfollowed rides exposed: %+v", page)
	}
	if err := env.app.Follows.Follow(ctx, alice.userID, bob.userID); err != nil {
		t.Fatal(err)
	}
	for _, row := range readHeadToHead(t, alice, bob).Records {
		if (row.Metric == "power" || row.Metric == "heart_rate") && row.Opponent != nil {
			t.Fatal("default follower settings exposed a restricted sensor record")
		}
	}
	policy := domain.DefaultVisibilityPolicy()
	policy.ByAudience[domain.AudienceFollowers][domain.CategoryPower] = true
	policy.ByAudience[domain.AudienceFollowers][domain.CategoryHR] = true
	if err := env.app.Visibility.SetUserPolicy(ctx, bob.userID, policy); err != nil {
		t.Fatal(err)
	}

	saveEffort := func(seg domain.SegmentID, act ride, elapsed, moving float64, offset int) {
		t.Helper()
		if err := env.app.Segments.SaveSegmentEffort(ctx, domain.SegmentEffort{
			ID: domain.SegmentEffortID(uuid.New()), SegmentID: seg, ActivityID: act.activity, ActivitySourceID: act.source,
			UserID: act.member, ElapsedS: elapsed, MovingS: moving, StartTime: start, StartOffset: offset, EndOffset: offset + 100,
		}); err != nil {
			t.Fatal(err)
		}
	}
	duplicate := a
	src, err := env.app.Activities.GetSource(ctx, a.source)
	if err != nil {
		t.Fatal(err)
	}
	src.ID, src.Provider, src.ExternalID = domain.SourceID(uuid.New()), "garmin", "same-ride"
	if err := env.app.Activities.SaveSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	duplicate.source = src.ID

	for _, tc := range []struct {
		name          string
		you, opponent float64
		scope         domain.SegmentScope
	}{
		{"Canal", 100, 120, domain.SegmentScopeInstance},
		{"Forest", 140, 100, domain.SegmentScopeInstance},
		{"Village", 200, 200, domain.SegmentScopeInstance},
		{"Private segment", 180, 160, domain.SegmentScopePrivate},
	} {
		seg := domain.Segment{
			ID: domain.SegmentID(uuid.New()), Source: domain.SegmentSourceNative, OwnerUserID: &bob.userID,
			Name: tc.name, Scope: tc.scope, ActivityType: domain.ActivityTypeRide,
			Polyline: "gt{}G_hzo@o}@_q@", PolylinePrecision: 5, DistanceM: 1200,
		}
		if err := env.app.Segments.SaveSegment(ctx, seg); err != nil {
			t.Fatal(err)
		}
		saveEffort(seg.ID, a, tc.you+30, tc.you-20, 0)
		saveEffort(seg.ID, a, tc.you, tc.you-10, 200)
		saveEffort(seg.ID, duplicate, 75, 75, 0)
		saveEffort(seg.ID, b, tc.opponent, 60, 0)
		saveEffort(seg.ID, private, 50, 50, 0)
	}
	raw := alice.upload("duplicate.gpx", performanceGPX(start, time.Second, 145, 260))
	if str(raw, "activity_id") != a.activity.String() || str(raw, "source_id") != a.source.String() {
		t.Fatal("duplicate upload did not reuse its original identity")
	}
	page := readHeadToHead(t, alice, bob)
	if len(page.Segments) != 3 || page.SegmentScore.Wins != 1 || page.SegmentScore.Losses != 1 ||
		page.SegmentScore.Ties != 1 || page.SegmentScore.Comparable != 3 {
		t.Fatalf("unexpected segment table or score: %+v", page.Segments)
	}
	canal := page.Segments[0]
	if canal.Name != "Canal" || canal.You.Value != 100 || canal.Opponent.Value != 120 ||
		canal.TimeDifference != 20 || math.Abs(canal.SpeedDifference-7.2) > 1e-9 || canal.Winner != "you" {
		t.Fatalf("elapsed time, canonical source or privacy mismatch: %+v", canal)
	}

	// A newly imported ride updates a record without changing the segment score.
	newRide := bob.upload("improvement.gpx", performanceGPX(start.AddDate(0, 0, 3), time.Second, 145, 320))
	newID := str(newRide, "activity_id")
	setITSegmentVisibility(bob, newID, "FOLLOWERS")
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(25 * time.Millisecond) {
		page = readHeadToHead(t, alice, bob)
		if page.RecordScore.Losses > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("new opponent import did not update the standings")
		}
	}
	for _, r := range page.Records {
		if r.Metric == "power" && r.Window == 300 {
			if r.Opponent == nil || r.Opponent.ActivityID != newID || r.Opponent.Value != 320 || r.Winner != "opponent" {
				t.Fatalf("private or old effort selected: %+v", r)
			}
		}
		if r.Metric == "heart_rate" && r.Winner != "" {
			t.Fatal("heart rate was scored")
		}
	}
	moderationPath := "/api/admin/activities/" + newID + "/hide"
	hide := map[string]any{"hidden": true}
	alice.do(http.MethodPost, moderationPath, hide, http.StatusForbidden)
	admin := env.user("compare-admin", domain.UserRoleAdmin)
	admin.post(moderationPath, hide, http.StatusOK)
	for _, row := range readHeadToHead(t, alice, bob).Records {
		if row.Opponent != nil && row.Opponent.ActivityID == newID {
			t.Fatal("moderated activity contributed a record")
		}
	}
	override := domain.VisibilityPolicy{ByAudience: map[domain.Audience]domain.CategorySet{
		domain.AudienceFollowers: {},
	}}
	if err := env.app.Visibility.SetActivityOverride(ctx, b.activity, &override); err != nil {
		t.Fatal(err)
	}
	if len(readHeadToHead(t, alice, bob).Segments) != 0 {
		t.Fatal("per-activity category override was ignored")
	}
	if err := env.app.Blocks.Block(ctx, bob.userID, alice.userID); err != nil {
		t.Fatal(err)
	}
	alice.do(http.MethodGet, endpoint+bob.name, nil, http.StatusNotFound)
	bob.do(http.MethodGet, endpoint+alice.name, nil, http.StatusNotFound)
}

func TestAPI_CyclingDistanceBackfill(t *testing.T) {
	env := newITDiscoveryEnv(t)
	startITConsumers(t, env, startFollowUpConsumer)
	rider := env.user("historic-cyclist", domain.UserRoleUser)
	start := time.Date(2020, 7, 10, 10, 0, 0, 0, time.UTC)
	var gpx strings.Builder
	gpx.WriteString(`<gpx version="1.1"><trk><name>Long ride</name><type>cycling</type><trkseg>`)
	for i := range 5501 {
		fmt.Fprintf(&gpx, `<trkpt lat="47.0" lon="%.6f"><ele>50</ele><time>%s</time><extensions><power>200</power></extensions></trkpt>`,
			8+float64(i)*0.00013, start.Add(time.Duration(i)*time.Second).Format(time.RFC3339))
	}
	gpx.WriteString(`</trkseg></trk></gpx>`)
	upload := rider.upload("long.gpx", []byte(gpx.String()))
	waitForPersonalPower(t, rider, 200)
	check := func(want bool) {
		t.Helper()
		rows, err := env.app.BestEfforts.ListPersonalRecords(t.Context(), rider.userID)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range []int{20000, 50000} {
			found := false
			for _, row := range rows {
				if row.Metric == domain.BestEffortMetricPace && row.WindowValue == target {
					found = row.ActivityID.String() == str(upload, "activity_id") && row.AchievedValue > 0
				}
			}
			if found != want {
				t.Fatalf("distance %d present = %t, want %t", target, found, want)
			}
		}
	}
	check(true)
	for _, value := range []int{20000, 50000} {
		if err := env.app.BestEfforts.DeleteForUserWindow(t.Context(), rider.userID, domain.ActivityTypeRide,
			domain.BestEffortMetricPace, domain.BestEffortWindowDistance, value); err != nil {
			t.Fatal(err)
		}
	}
	check(false)
	for _, expected := range []int{1, 0} {
		count, err := env.app.ComputeBestEfforts.BackfillCyclingDistances(t.Context(), env.app.BestEfforts)
		if err != nil || count != expected {
			t.Fatalf("backfill = %d, %v; want %d", count, err, expected)
		}
		check(true)
	}
}
