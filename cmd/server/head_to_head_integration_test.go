//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type comparedSegmentResponse struct {
	ID            string
	Name          string
	You, Opponent struct {
		ActivityID string `json:"activity_id"`
		Value      float64
	}
	Winner          string
	YouSpeed        float64 `json:"you_speed_kmh"`
	OpponentSpeed   float64 `json:"opponent_speed_kmh"`
	TimeDifference  float64 `json:"time_difference_s"`
	SpeedDifference float64 `json:"speed_difference_kmh"`
}

type comparedRecordResponse struct {
	Metric        string
	Window        int `json:"window_value"`
	You, Opponent *struct {
		ActivityID string `json:"activity_id"`
		Value      float64
	}
	Winner string
}

type headToHeadResponse struct {
	Segments     []comparedSegmentResponse
	Records      []comparedRecordResponse
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
	admin := env.user("compare-admin", domain.UserRoleAdmin)
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
	a := upload(alice, 0, 260, "PUBLIC")
	bStart := start.AddDate(0, 0, 1)
	bID := importGarminFITFixture(t, env, bob, bStart, garminTestFIT(t, bStart))
	garmin, err := env.app.Activities.FindOwnedSourceByExternalID(ctx, bob.userID, garminProvider, nil, "garmin-fit")
	if err != nil {
		t.Fatal(err)
	}
	b := ride{bID, garmin.ID, bob.userID}
	setITSegmentVisibility(bob, bID.String(), "PUBLIC")
	waitForPersonalPower(t, bob, 225)
	hidden := upload(bob, 2, 390, "PRIVATE")
	admin.post("/api/admin/activities/"+hidden.activity.String()+"/hide", map[string]any{"hidden": true}, http.StatusOK)
	restricted := upload(bob, 4, 410, "PUBLIC")
	restrictedActivity, err := env.app.Activities.GetActivity(ctx, restricted.activity)
	if err != nil {
		t.Fatal(err)
	}
	restrictedActivity.ProviderRestricted = true
	if err := env.app.Activities.SaveActivity(ctx, restrictedActivity); err != nil {
		t.Fatal(err)
	}
	const endpoint = "/api/head-to-head?member="
	env.anon().do(http.MethodGet, endpoint+bob.name, nil, http.StatusUnauthorized)
	alice.do(http.MethodGet, endpoint+alice.name, nil, http.StatusBadRequest)
	alice.do(http.MethodGet, endpoint+"unknown-rider", nil, http.StatusNotFound)

	saveEffort := func(seg domain.SegmentID, act ride, elapsed, moving float64, offset int, ref *string) {
		t.Helper()
		if err := env.app.Segments.SaveSegmentEffort(ctx, domain.SegmentEffort{
			ID: domain.SegmentEffortID(uuid.New()), SegmentID: seg, ActivityID: act.activity, ActivitySourceID: act.source,
			UserID: act.member, ElapsedS: elapsed, MovingS: moving, StartTime: start, StartOffset: offset, EndOffset: offset + 100,
			ProviderEffortExternalID: ref,
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
	stravaCopy := a
	src.ID, src.Provider, src.ExternalID = domain.SourceID(uuid.New()), "strava", "same-ride"
	if err := env.app.Activities.SaveSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	stravaCopy.source = src.ID

	var canalID domain.SegmentID
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
		if tc.name == "Canal" {
			seg.Source, seg.OwnerUserID = domain.SegmentSourceStravaCatalog, nil
			seg.ExternalID, seg.CatalogProvider = new("12345"), new("test-map")
			canalID = seg.ID
		}
		if err := env.app.Segments.SaveSegment(ctx, seg); err != nil {
			t.Fatal(err)
		}
		saveEffort(seg.ID, a, tc.you+30, tc.you-20, 0, nil)
		saveEffort(seg.ID, a, tc.you, tc.you-10, 200, nil)
		saveEffort(seg.ID, duplicate, 75, 75, 0, nil)
		saveEffort(seg.ID, b, tc.opponent, 60, 0, nil)
		saveEffort(seg.ID, hidden, 50, 50, 0, nil)
		saveEffort(seg.ID, restricted, 40, 40, 0, nil)
		if seg.ID == canalID {
			saveEffort(seg.ID, b, 45, 45, 250, new("mirrored-effort"))
			// Another catalog definition with the same name has no eligible effort for Alice.
			seg.ID, seg.ExternalID = domain.SegmentID(uuid.New()), new("12346")
			if err := env.app.Segments.SaveSegment(ctx, seg); err != nil {
				t.Fatal(err)
			}
			saveEffort(seg.ID, stravaCopy, 40, 40, 0, nil)
			saveEffort(seg.ID, b, 65, 65, 0, nil)
		}
	}
	raw := alice.upload("duplicate.gpx", performanceGPX(start, time.Second, 145, 260))
	if str(raw, "activity_id") != a.activity.String() || str(raw, "source_id") != a.source.String() {
		t.Fatal("duplicate upload did not reuse its original identity")
	}
	page := readHeadToHead(t, alice, bob)
	// Intersect the eligible personal segment lists by ID and compare their best times.
	aSegments, err := env.app.Segments.ListUserSegmentsByEffortCount(ctx, alice.userID, "ride", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	bSegments, err := env.app.Segments.ListUserSegmentsByEffortCount(port.WithActivityReadViewer(ctx, false), bob.userID, "ride", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	intersection := 0
	for _, theirs := range bSegments {
		for _, yours := range aSegments {
			if yours.ID != theirs.ID || yours.Name == "Private segment" {
				continue
			}
			intersection++
			if !slices.ContainsFunc(page.Segments, func(row comparedSegmentResponse) bool {
				return row.ID == yours.ID.String() && row.You.Value == yours.BestElapsedS && row.Opponent.Value == theirs.BestElapsedS
			}) {
				t.Errorf("canonical segment %s missing or times differ from personal segments", yours.ID)
			}
		}
	}
	if intersection != 3 {
		t.Fatalf("eligible personal segment intersection = %d, want 3", intersection)
	}
	if len(page.Segments) != 3 || page.SegmentScore.Wins != 1 || page.SegmentScore.Losses != 1 ||
		page.SegmentScore.Ties != 1 || page.SegmentScore.Comparable != 3 {
		t.Fatalf("unexpected segment table or score: %+v", page.Segments)
	}
	canal := page.Segments[0]
	if canal.ID != canalID.String() || canal.Name != "Canal" || canal.You.Value != 100 || canal.Opponent.Value != 120 ||
		canal.YouSpeed != 43.2 || canal.OpponentSpeed != 36 ||
		canal.TimeDifference != 20 || math.Abs(canal.SpeedDifference-7.2) > 1e-9 || canal.Winner != "you" {
		t.Fatalf("elapsed time, canonical source or privacy mismatch: %+v", canal)
	}
	if board := segmentBoard(t, alice, canal.ID); len(board) != 2 || board[0].Elapsed != canal.You.Value || board[1].Elapsed != canal.Opponent.Value {
		t.Fatalf("comparison differs from the catalog leaderboard: %+v", board)
	}
	if !slices.ContainsFunc(readHeadToHead(t, bob, alice).Segments, func(row comparedSegmentResponse) bool {
		return row.ID == canal.ID && row.Opponent.ActivityID == a.activity.String() && row.Opponent.Value == 100
	}) {
		t.Fatal("the public upload is missing from the reverse comparison")
	}

	// A newly imported ride updates a record without changing the segment score.
	newRide := bob.upload("improvement.gpx", performanceGPX(start.AddDate(0, 0, 3), time.Second, 145, 320))
	newID := str(newRide, "activity_id")

	// Wait for the new public import; the provider-restricted PR stays excluded.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(25 * time.Millisecond) {
		rows := readRecordBoard(t, alice, "/api/records/leaderboards/ride/power/duration/300")
		if len(rows) == 2 && rows[0].Result.ActivityID == newID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("new opponent import did not update the leaderboard")
		}
	}
	page = readHeadToHead(t, alice, bob)
	if page.RecordScore.Losses == 0 || page.SegmentScore.Comparable != 3 {
		t.Fatalf("new import scores: records %+v, segments %+v", page.RecordScore, page.SegmentScore)
	}
	for _, r := range page.Records {
		if r.Metric == "power" && r.Window == 300 {
			if r.Opponent == nil || r.Opponent.ActivityID != newID || r.Opponent.Value != 320 || r.Winner != "opponent" {
				t.Fatalf("ineligible or old effort selected: %+v", r)
			}
		}
		if r.Metric == "heart_rate" && r.Winner != "" {
			t.Fatal("heart rate was scored")
		}
	}
	moderationPath := "/api/admin/activities/" + newID + "/hide"
	hide := map[string]any{"hidden": true}
	alice.do(http.MethodPost, moderationPath, hide, http.StatusForbidden)
	admin.post(moderationPath, hide, http.StatusOK)
	for _, row := range readHeadToHead(t, alice, bob).Records {
		if row.Metric == "power" && row.Window == 300 && (row.Opponent == nil || row.Opponent.ActivityID != bID.String() || row.Opponent.Value != 225) {
			t.Fatalf("comparison did not fall back to the eligible Garmin record: %+v", row)
		}
	}
	// A third signed-in rider can compare this pair without inheriting either
	// selected rider's access to hidden or provider-restricted activities.
	peerPath := "/api/head-to-head?first=" + bob.name + "&second=" + alice.name
	peerBody := admin.do(http.MethodGet, peerPath, nil, http.StatusOK)
	var peer headToHeadResponse
	if err := json.Unmarshal(peerBody, &peer); err != nil {
		t.Fatal(err)
	}
	if len(peer.Segments) != 3 || peer.Segments[0].ID != canal.ID || peer.Segments[0].You.Value != 120 {
		t.Fatalf("wrong comparison for the selected pair: %s", peerBody)
	}
	for _, row := range peer.Records {
		if row.Metric == "power" && row.Window == 300 && (row.You == nil || row.You.ActivityID != bID.String() || row.You.Value != 225) {
			t.Fatalf("third-party comparison included a moderated or restricted activity: %+v", row)
		}
	}
	admin.do(http.MethodGet, "/api/head-to-head?first="+alice.name+"&second="+alice.name, nil, http.StatusBadRequest)

	alice.do(http.MethodGet, endpoint+bob.name, nil, http.StatusOK)
	bob.do(http.MethodGet, endpoint+alice.name, nil, http.StatusOK)
}

func TestAPI_HeadToHeadHistoricalDurationRecords(t *testing.T) {
	ctx := t.Context()
	env := newITDiscoveryEnv(t)
	startITConsumers(t, env, startFollowUpConsumer)
	alice := env.user("duration-alice", domain.UserRoleUser)
	bob := env.user("duration-bob", domain.UserRoleUser)
	start := time.Date(2021, 2, 1, 8, 0, 0, 0, time.UTC)
	gpx := performanceGPX(start, 2*time.Second, 145, 260)
	gpx = []byte(strings.Replace(string(gpx), start.Add(20*time.Second).Format(time.RFC3339), start.Add(21*time.Second).Format(time.RFC3339), 1))
	a := alice.upload("irregular.gpx", gpx)
	b := bob.upload("record.gpx", performanceGPX(start, time.Second, 140, 240))
	waitForPersonalPower(t, alice, 260)
	waitForPersonalPower(t, bob, 240)

	// Preserve a record as the historical sample-count calculator stored it.
	activityID := domain.ActivityID(uuid.MustParse(str(a, "activity_id")))
	efforts, err := env.app.BestEfforts.ListForActivity(ctx, activityID)
	if err != nil {
		t.Fatal(err)
	}
	idx := slices.IndexFunc(efforts, func(e domain.BestEffort) bool {
		return e.Metric == domain.BestEffortMetricPower && e.WindowValue == 300
	})
	if idx < 0 {
		t.Fatal("irregular recording did not produce a personal record")
	}
	legacy := efforts[idx]
	legacy.DurationS = 598 // 300 samples with endpoints 598 seconds apart.
	if err := env.app.BestEfforts.DeleteForUserWindow(ctx, alice.userID, legacy.ActivityType, legacy.Metric, legacy.WindowKind, legacy.WindowValue); err != nil {
		t.Fatal(err)
	}
	if err := env.app.BestEfforts.SaveEfforts(ctx, []domain.BestEffort{legacy}); err != nil {
		t.Fatal(err)
	}
	waitForPersonalPower(t, alice, 260)
	board := readRecordBoard(t, alice, "/api/records/leaderboards/ride/power/duration/300")
	if len(board) != 2 || board[0].Result.ActivityID != str(a, "activity_id") || board[1].Result.ActivityID != str(b, "activity_id") {
		t.Fatalf("records leaderboard is missing a personal best: %+v", board)
	}
	page := readHeadToHead(t, alice, bob)
	for _, row := range page.Records {
		if row.Metric == "power" && row.Window == 300 {
			if row.You == nil || row.You.ActivityID != board[0].Result.ActivityID || row.You.Value != board[0].Result.AchievedValue {
				t.Errorf("stored power record with duration %.0f seconds is missing or differs from Records: %+v", legacy.DurationS, row.You)
			}
			if row.Opponent == nil || row.Opponent.ActivityID != board[1].Result.ActivityID || row.Opponent.Value != board[1].Result.AchievedValue || row.Winner != "you" {
				t.Errorf("opponent power record differs from Records or the winner is incorrect: %+v", row)
			}
			return
		}
	}
	t.Fatal("300-second power category missing")
}

func TestAPI_CyclingDistanceBackfill(t *testing.T) {
	env := newITDiscoveryEnv(t)
	startITConsumers(t, env, startFollowUpConsumer)
	rider := env.user("historic-cyclist", domain.UserRoleUser)
	opponent := env.user("historic-opponent", domain.UserRoleUser)
	start := time.Date(2020, 7, 10, 10, 0, 0, 0, time.UTC)
	var gpx strings.Builder
	gpx.WriteString(`<gpx version="1.1"><trk><name>Long ride</name><type>cycling</type><trkseg>`)
	for i := range 5501 {
		fmt.Fprintf(&gpx, `<trkpt lat="47.0" lon="%.6f"><ele>50</ele><time>%s</time><extensions><power>200</power></extensions></trkpt>`,
			8+float64(i)*0.00013, start.Add(time.Duration(i)*time.Second).Format(time.RFC3339))
	}
	gpx.WriteString(`</trkseg></trk></gpx>`)
	upload := rider.upload("long.gpx", []byte(gpx.String()))
	other := opponent.upload("long.gpx", []byte(gpx.String()))
	waitForPersonalPower(t, rider, 200)
	waitForPersonalPower(t, opponent, 200)
	check := func(want bool) {
		t.Helper()
		rows, err := env.app.BestEfforts.ListPersonalRecords(t.Context(), rider.userID)
		if err != nil {
			t.Fatal(err)
		}
		page := readHeadToHead(t, rider, opponent)
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
			idx := slices.IndexFunc(page.Records, func(row comparedRecordResponse) bool {
				return row.Metric == "pace" && row.Window == target
			})
			if idx < 0 {
				t.Fatalf("distance record bucket %d missing", target)
			}
			row := page.Records[idx]
			if row.Opponent == nil || row.Opponent.ActivityID != str(other, "activity_id") || (row.You != nil) != want {
				t.Fatalf("distance comparison %d has incorrect availability: %+v", target, row)
			}
			if want && (row.You.ActivityID != str(upload, "activity_id") || row.Winner != "tie" || row.You.Value != row.Opponent.Value) {
				t.Fatalf("distance comparison %d did not reuse both recorded efforts: %+v", target, row)
			}
			if !want && row.Winner != "" {
				t.Fatalf("distance comparison %d was scored with a missing record: %+v", target, row)
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
