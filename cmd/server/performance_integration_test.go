//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

type performanceProperties struct {
	Density   int
	Speed     float64
	HeartRate *float64 `json:"heart_rate"`
	Power     *float64
}

type performanceResponse struct {
	Matched   int
	WithTrack int `json:"with_track"`
	Pending   int
	GeoJSON   struct {
		Features []struct{ Properties performanceProperties }
	}
}

func TestAPI_MemberHeatmapPreparation(t *testing.T) {
	env := newITestEnv(t)
	viewer := env.user("heatmap-viewer", domain.UserRoleUser)
	rider := env.user("heatmap-rider", domain.UserRoleUser)
	start := time.Date(2020, 3, 2, 10, 0, 0, 0, time.UTC)
	viewer.upload("viewer.gpx", performanceGPX(start, time.Second, 155, 0))
	const path = "/api/activities/heatmap/performance"
	if got := readPerformanceMap(t, viewer, ""); got.WithTrack != 1 {
		t.Fatalf("viewer cache did not warm up: %+v", got)
	}

	// One more ride than the maximum preparation batch, with distinct stream times.
	const total = 25
	for i := range total {
		rider.upload("rider.gpx", performanceGPX(start.AddDate(0, 0, i), time.Second, 130, 0))
	}
	conn, err := pgx.Connect(t.Context(), os.Getenv("CAIRN_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(t.Context()) }()
	cacheCount := func(want int) {
		t.Helper()
		var cached int
		err := conn.QueryRow(t.Context(), `SELECT count(*) FROM performance_heatmap_cache h
			JOIN activities a ON a.id=h.activity_id WHERE a.user_id=$1`, rider.userID.UUID()).Scan(&cached)
		if err != nil || cached != want {
			t.Fatalf("member cache has %d rows, want %d (error: %v)", cached, want, err)
		}
	}
	cacheCount(0)
	overview := path + "?member=" + rider.name
	body := viewer.do(http.MethodGet, overview, nil, http.StatusAccepted)
	var result performanceResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Matched != total || result.Pending <= 0 || result.Pending >= total {
		t.Errorf("expected a decrease in pending rides after the first batch: %s", body)
	} else {
		cacheCount(total - result.Pending)
	}
	if got := readPerformanceMap(t, viewer, "member="+rider.name); got.WithTrack != total || len(got.GeoJSON.Features) == 0 {
		t.Fatalf("cold member cache did not finish: %+v", got)
	}
	cacheCount(total)
	body = viewer.do(http.MethodGet, overview+"&level=2&west=7.85&east=8.15&south=46.85&north=47.15&from=2020-03-03", nil, http.StatusOK)
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Matched != total-1 || result.WithTrack != total-1 || len(result.GeoJSON.Features) == 0 {
		t.Fatalf("filtered member viewport did not use the member cache: %s", body)
	}
	if got := readPerformanceMap(t, viewer, ""); got.WithTrack != 1 {
		t.Fatalf("switching back to the viewer returned the wrong cache: %+v", got)
	}

	rider.upload("uncached.gpx", performanceGPX(start.AddDate(0, 0, total), time.Second, 130, 0))
	// A successful SQL command can skip the row entirely.
	if _, err := conn.Exec(t.Context(), `
		CREATE FUNCTION skip_heatmap_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$;
		CREATE TRIGGER skip_heatmap_write BEFORE INSERT ON performance_heatmap_cache
		FOR EACH ROW EXECUTE FUNCTION skip_heatmap_write();
	`); err != nil {
		t.Fatal(err)
	}
	viewer.do(http.MethodGet, overview, nil, http.StatusInternalServerError)
	cacheCount(total)
	if _, err := conn.Exec(t.Context(), `DROP TRIGGER skip_heatmap_write ON performance_heatmap_cache`); err != nil {
		t.Fatal(err)
	}
	if got := readPerformanceMap(t, viewer, "member="+rider.name); got.WithTrack != total+1 {
		t.Fatalf("member cache did not recover: %+v", got)
	}
	cacheCount(total + 1)
}

func readPerformanceMap(t *testing.T, client *itestClient, query string) performanceResponse {
	t.Helper()
	for range 35 {
		body := client.do(http.MethodGet, "/api/activities/heatmap/performance?"+query, nil, http.StatusOK, http.StatusAccepted)
		var result performanceResponse
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		if result.Pending == 0 {
			return result
		}
	}
	t.Fatal("map did not finish indexing")
	return performanceResponse{}
}

func TestAPI_CumulativeHeatmap(t *testing.T) {
	env := newITestEnv(t)
	alice := env.user("heatmap-alice", domain.UserRoleUser)
	bob := env.user("heatmap-bob", domain.UserRoleUser)
	early := time.Date(2011, 2, 5, 12, 0, 0, 0, time.UTC)
	fast := performanceGPX(early, time.Second, 180, 250)
	first := alice.upload("fast.gpx", fast)
	other := alice.upload("slow.gpx", performanceGPX(early.AddDate(6, 0, 0), 3*time.Second, 120, 0))
	repeat := alice.upload("fast-again.gpx", fast)
	if str(first, "activity_id") != str(repeat, "activity_id") {
		t.Fatal("the same file created two activities")
	}

	query := "level=2&west=7.85&east=8.15&south=46.85&north=47.15&member=" + alice.name
	env.anon().do(http.MethodGet, "/api/activities/heatmap/performance?"+query, nil, http.StatusUnauthorized)
	result := readPerformanceMap(t, bob, query)
	if result.Matched != 2 || result.WithTrack != 2 || len(result.GeoJSON.Features) == 0 {
		t.Fatalf("wrong all-time ride counts: %+v", result)
	}
	findOverlap := func(data performanceResponse, passes int) performanceProperties {
		t.Helper()
		for _, row := range data.GeoJSON.Features {
			if row.Properties.Density == passes {
				return row.Properties
			}
		}
		t.Fatalf("no heatmap bin with %d passages", passes)
		return performanceProperties{}
	}
	properties := findOverlap(result, 2)
	// Identical displacement and 1s vs 3s sample intervals make HR 135,
	// with watts taking their weight exclusively from the first ride.
	if properties.HeartRate == nil || *properties.HeartRate != 135 || properties.Power == nil || *properties.Power != 250 || properties.Speed < 12 || properties.Speed > 18 {
		t.Fatalf("bad cumulative values: %+v", properties)
	}
	checkEmpty := readPerformanceMap(t, bob, "level=2&west=20&east=21&south=40&north=41&member="+alice.name)
	if len(checkEmpty.GeoJSON.Features) != 0 || checkEmpty.WithTrack != 2 {
		t.Fatal("viewport either dropped history or returned out-of-view bins")
	}
	third := alice.upload("missing-sensors.gpx", performanceGPX(early.AddDate(12, 0, 0), time.Second, 0, 0))
	updated := readPerformanceMap(t, bob, query)
	missing := findOverlap(updated, 3)
	if updated.Matched != 3 || missing.HeartRate == nil || *missing.HeartRate != 135 || missing.Power == nil || *missing.Power != 250 {
		t.Fatalf("new activity or missing-channel weights: %+v", missing)
	}
	alice.do(http.MethodDelete, "/api/activities/"+str(other, "activity_id"), nil, http.StatusNoContent, http.StatusOK)
	moderator := env.user("heatmap-admin", domain.UserRoleAdmin)
	moderator.post("/api/admin/activities/"+str(third, "activity_id")+"/hide", map[string]any{"hidden": true}, http.StatusOK)
	after := readPerformanceMap(t, bob, query)
	single := findOverlap(after, 1)
	if after.Matched != 1 || single.HeartRate == nil || *single.HeartRate != 180 {
		t.Fatalf("stale cache after deletion/moderation: %+v", single)
	}

	sourceID, err := domain.ParseUUID[domain.SourceID](str(first, "source_id"))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := env.app.Streams.QueryStream(t.Context(), domain.StreamQuery{ActivitySourceID: sourceID})
	if err != nil {
		t.Fatal(err)
	}
	for i := range stream.Samples {
		stream.Samples[i].HeartRateBpm = new(int16(160))
	}
	if err := env.app.Streams.WriteStream(t.Context(), sourceID, stream.Samples); err != nil {
		t.Fatal(err)
	}
	revised := findOverlap(readPerformanceMap(t, bob, query), 1)
	if revised.HeartRate == nil || *revised.HeartRate != 160 {
		t.Fatal("replaced stream did not invalidate cached bins")
	}
}

func performanceGPX(start time.Time, period time.Duration, hr, power int) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><gpx version="1.1" creator="itest"><trk><name>Performance ride</name><type>cycling</type><trkseg>`)
	for i := range 500 {
		fmt.Fprintf(&b, `<trkpt lat="47.002" lon="%.7f"><ele>%.2f</ele><time>%s</time><extensions>`,
			8.001+float64(i)*0.0001, 100+float64(i)*0.12, start.Add(time.Duration(i)*period).UTC().Format(time.RFC3339Nano))
		if hr > 0 {
			fmt.Fprintf(&b, "<TrackPointExtension><hr>%d</hr></TrackPointExtension>", hr)
		}
		if power > 0 {
			fmt.Fprintf(&b, "<power>%d</power>", power)
		}
		b.WriteString("</extensions></trkpt>")
	}
	b.WriteString("</trkseg></trk></gpx>")
	return []byte(b.String())
}

type rankedRecord struct {
	ActivityType string `json:"activity_type"`
	Metric       string
	WindowKind   string `json:"window_kind"`
	WindowValue  int    `json:"window_value"`
	MemberCount  int    `json:"member_count"`
	Result       *struct {
		Rank          int
		Username      string
		AchievedValue float64 `json:"achieved_value"`
		ActivityID    string  `json:"activity_id"`
	}
}

func readRecordBoard(t *testing.T, client *itestClient, path string) []rankedRecord {
	t.Helper()
	var board struct{ Records []rankedRecord }
	if err := json.Unmarshal(client.do(http.MethodGet, path, nil, http.StatusOK), &board); err != nil {
		t.Fatal(err)
	}
	return board.Records
}

func TestAPI_CommunityRecordRankings(t *testing.T) {
	env := newITDiscoveryEnv(t)
	startITConsumers(t, env, startFollowUpConsumer)
	alice := env.user("records-alice", domain.UserRoleUser)
	bob := env.user("records-bob", domain.UserRoleUser)
	cara := env.user("records-cara", domain.UserRoleUser)
	start := time.Date(2020, 3, 2, 10, 0, 0, 0, time.UTC)
	award := func(c *itestClient, day int, watts int) map[string]any {
		return c.upload("record.gpx", performanceGPX(start.AddDate(0, 0, day), time.Second, 145, watts))
	}
	first := award(alice, 0, 280)
	second := award(bob, 1, 280)
	older := award(alice, 2, 250)
	estimated := award(cara, 3, 420)
	noMeter := award(cara, 4, 0)
	waitForPersonalPower(t, cara, 420) // Let the import finish before changing fixture provenance.
	estimatedSource, err := domain.ParseUUID[domain.SourceID](str(estimated, "source_id"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := env.app.Activities.GetSource(t.Context(), estimatedSource)
	if err != nil {
		t.Fatal(err)
	}
	src.Parsed.PowerRecorded = new(false)
	if err := env.app.Activities.SaveSource(t.Context(), src); err != nil {
		t.Fatal(err)
	}
	invalidActivity, _ := domain.ParseUUID[domain.ActivityID](str(noMeter, "activity_id"))
	invalidSource, _ := domain.ParseUUID[domain.SourceID](str(noMeter, "source_id"))
	if err := env.app.BestEfforts.SaveEfforts(t.Context(), []domain.BestEffort{{
		ID: domain.BestEffortID(uuid.New()), ActivityID: invalidActivity, ActivitySourceID: invalidSource,
		UserID: cara.userID, ActivityType: domain.ActivityTypeRide, Metric: domain.BestEffortMetricPower,
		WindowKind: domain.BestEffortWindowDuration, WindowValue: 300, AchievedValue: math.NaN(), Timestamp: start,
	}}); err != nil {
		t.Fatal(err)
	}

	const overview = "/api/records/leaderboards"
	const detail = overview + "/ride/power/duration/300"
	for _, path := range []string{overview, detail} {
		env.anon().do(http.MethodGet, path, nil, http.StatusUnauthorized)
	}
	waitForPersonalPower(t, alice, 280)
	waitForPersonalPower(t, bob, 280)
	rows := readRecordBoard(t, bob, detail)
	if len(rows) != 2 || rows[0].MemberCount != 2 || rows[0].Result.Rank != 1 || rows[1].Result.Rank != 1 ||
		rows[0].Result.ActivityID != str(first, "activity_id") || rows[1].Result.ActivityID != str(second, "activity_id") {
		t.Fatalf("tie, estimated power or missing data: %s", bob.do(http.MethodGet, detail, nil, http.StatusOK))
	}

	find := func() rankedRecord {
		t.Helper()
		for _, row := range readRecordBoard(t, bob, overview) {
			if row.ActivityType == "ride" && row.Metric == "power" && row.WindowKind == "duration" && row.WindowValue == 300 {
				return row
			}
		}
		t.Fatal("300-second power category missing")
		return rankedRecord{}
	}
	if row := find(); row.MemberCount != 2 || row.Result.Username != alice.name {
		t.Fatalf("wrong overview winner: %+v", row)
	}
	temp := award(bob, 5, 320)
	waitForPersonalPower(t, bob, 320)
	if row := find(); row.Result.ActivityID != str(temp, "activity_id") {
		t.Fatal("new import did not replace the record")
	}
	bob.do(http.MethodDelete, "/api/activities/"+str(temp, "activity_id"), nil, http.StatusOK, http.StatusNoContent)
	alice.do(http.MethodDelete, "/api/activities/"+str(first, "activity_id"), nil, http.StatusOK, http.StatusNoContent)
	waitForPersonalPower(t, alice, 250)
	rows = readRecordBoard(t, bob, detail)
	if len(rows) != 2 || rows[0].Result.Rank != 1 || rows[0].Result.Username != bob.name || rows[1].Result.Rank != 2 || rows[1].Result.ActivityID != str(older, "activity_id") {
		t.Fatal("deleted records did not fall back to each rider's next-best result")
	}

	// A lower pace (seconds per kilometre) beats a higher one.
	for _, data := range []struct {
		client *itestClient
		upload map[string]any
		value  float64
	}{{alice, older, 150}, {bob, second, 160}} {
		aid, _ := domain.ParseUUID[domain.ActivityID](str(data.upload, "activity_id"))
		sid, _ := domain.ParseUUID[domain.SourceID](str(data.upload, "source_id"))
		if err := env.app.BestEfforts.SaveEfforts(t.Context(), []domain.BestEffort{{
			ID: domain.BestEffortID(uuid.New()), ActivityID: aid, ActivitySourceID: sid, UserID: data.client.userID, ActivityType: domain.ActivityTypeRide,
			Metric: domain.BestEffortMetricPace, WindowKind: domain.BestEffortWindowDistance, WindowValue: 375,
			AchievedValue: data.value, Timestamp: start,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	rows = readRecordBoard(t, bob, overview+"/ride/pace/distance/375")
	if len(rows) != 2 || rows[0].Result.Username != alice.name || rows[0].Result.AchievedValue != 150 {
		t.Fatal("pace was not ranked ascending")
	}
}

func waitForPersonalPower(t *testing.T, client *itestClient, watts float64) {
	t.Helper()
	for until := time.Now().Add(20 * time.Second); time.Now().Before(until); {
		var page struct {
			Records []struct {
				Metric   string
				Window   int     `json:"window_value"`
				Achieved float64 `json:"achieved_value"`
			}
		}
		if err := json.Unmarshal(client.do(http.MethodGet, "/api/records", nil, http.StatusOK), &page); err != nil {
			t.Fatal(err)
		}
		for _, row := range page.Records {
			if row.Metric == "power" && row.Window == 300 && math.Abs(row.Achieved-watts) < 1e-9 {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s did not get a personal best of %.0f W", client.name, watts)
}
