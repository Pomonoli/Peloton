//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/export"
	"github.com/opencairn-org/cairn-core/internal/fileimport"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
	gearuc "github.com/opencairn-org/cairn-core/internal/usecase/gear"
)

func TestAPI_ActivityMovingTimeBackfill(t *testing.T) {
	env := newITestEnv(t)
	rider := env.user("timing-rider", domain.UserRoleUser)
	viewer := env.user("timing-viewer", domain.UserRoleUser)
	ctx := t.Context()
	bike, err := env.app.Gear.Create(ctx, rider.userID, gearuc.CreateInput{Kind: domain.GearKindBike, Name: "Road bike"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.app.Gear.SetDefault(ctx, rider.userID, domain.ActivityTypeRide, bike.ID); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2025, 6, 8, 8, 0, 0, 0, time.UTC)
	var gpx strings.Builder
	gpx.WriteString("<gpx><trk><name>Sunday ride</name><type>cycling</type><trkseg>")
	var metres float64
	for i := range 401 {
		// Two traffic lights and a longer stop during a 23.4 km/h ride.
		if i > 0 && !(i > 80 && i <= 95) && !(i > 150 && i <= 165) && !(i > 250 && i <= 310) {
			metres += 6.5
		}
		fmt.Fprintf(&gpx, `<trkpt lat="%.8f" lon="4.4"><time>%s</time></trkpt>`,
			50.8+metres*180/(math.Pi*domain.EarthRadiusMeters), start.Add(time.Duration(i)*time.Second).Format(time.RFC3339))
	}
	gpx.WriteString("</trkseg></trk></gpx>")
	raw := []byte(gpx.String())
	uploaded := rider.upload("ride.gpx", raw)
	id, err := domain.ParseUUID[domain.ActivityID](str(uploaded, "activity_id"))
	if err != nil {
		t.Fatal(err)
	}
	sourceID, err := domain.ParseUUID[domain.SourceID](str(uploaded, "source_id"))
	if err != nil {
		t.Fatal(err)
	}
	act, err := env.app.Activities.GetActivity(ctx, id)
	if err != nil || act.MovingDuration != 310*time.Second || act.ElapsedDuration != 400*time.Second ||
		act.Summary.AvgSpeedMps == nil || math.Abs(*act.Summary.AvgSpeedMps-6.5) > .01 || act.Summary.MaxSpeedMps == nil {
		t.Fatalf("import timing: %+v, error %v", act, err)
	}
	average := *act.Summary.AvgSpeedMps
	garmin, _, err := garminPayload(domain.GarminActivity{Moving: 360 * time.Second, Type: "road_cycling"}, port.UploadInput{
		Filename: "garmin.gpx", Raw: raw,
	})
	if err != nil || garmin.GetMovingDuration().AsDuration() != 310*time.Second {
		t.Fatalf("Garmin GPX moving time: %s, error %v", garmin.GetMovingDuration().AsDuration(), err)
	}

	if err := env.app.ComputeBestEfforts.Recalculate(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	source, err := env.app.Activities.GetSource(ctx, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	query := domain.StreamQuery{ActivitySourceID: sourceID, Resolution: domain.StreamResolutionRaw}
	stream, err := env.app.Streams.QueryStream(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	records, err := env.app.Stats.PersonalRecords(ctx, rider.userID)
	if err != nil || len(records) == 0 {
		t.Fatalf("personal records = %v, error %v", records, err)
	}

	// Simulate a row produced before the fix. The source and GPS stay as imported.
	if _, err := env.pool.Exec(ctx, `UPDATE activities SET moving_duration_s = elapsed_duration_s, avg_speed_mps = 1, tss = 50
		WHERE id = $1`, id.UUID()); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE gear SET total_moving_s = 400 WHERE id = $1`, bike.ID.UUID()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{1, 0} {
		count, err := backfillMovingTime(ctx, env.pool, nil)
		if err != nil || count != want {
			t.Fatalf("backfill changed %d, want %d, error %v", count, want, err)
		}
	}
	if bike, err := env.app.Gear.Get(ctx, rider.userID, bike.ID); err != nil || bike.TotalMoving != 310*time.Second {
		t.Fatalf("gear moving time: %s, error %v", bike.TotalMoving, err)
	}

	after, err := env.app.Activities.GetActivity(ctx, id)
	act.Summary.TSS = new(38.75)
	if err != nil || after.MovingDuration != act.MovingDuration || after.ElapsedDuration != act.ElapsedDuration ||
		!reflect.DeepEqual(after.Summary, act.Summary) {
		t.Fatalf("backfill summary: %+v, error %v", after, err)
	}
	if actual, err := env.app.Activities.GetSource(ctx, sourceID); err != nil || !reflect.DeepEqual(actual, source) {
		t.Fatalf("source changed, error %v", err)
	}
	if actual, err := env.app.Streams.QueryStream(ctx, query); err != nil || !reflect.DeepEqual(actual, stream) {
		t.Fatalf("GPS stream changed, error %v", err)
	}
	if actual, err := env.app.Stats.PersonalRecords(ctx, rider.userID); err != nil || !reflect.DeepEqual(actual, records) {
		t.Fatalf("personal records changed, error %v", err)
	}

	type activityStats struct {
		ID      string  `json:"id"`
		Elapsed int     `json:"elapsed_duration_s"`
		Moving  int     `json:"moving_duration_s"`
		Speed   float64 `json:"avg_speed_mps"`
	}
	want := activityStats{id.String(), 400, 310, average}
	for _, route := range []struct{ path, key string }{
		{"/api/activities/feed?member=timing-rider", "activities"},
		{"/api/overview?member=timing-rider", "recent"},
	} {
		var result map[string]json.RawMessage
		body := viewer.do(http.MethodGet, route.path, nil, http.StatusOK)
		var rows []activityStats
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(result[route.key], &rows); err != nil || len(rows) != 1 || rows[0] != want {
			t.Fatalf("%s summary = %+v, want %+v, error %v", route.path, rows, want, err)
		}
	}
	var stats struct {
		Records map[string]activityStats `json:"records"`
	}
	if err := json.Unmarshal(viewer.do(http.MethodGet, "/api/stats?member=timing-rider", nil, http.StatusOK), &stats); err != nil ||
		stats.Records["longest_distance"] != want {
		t.Fatalf("Stats records: %+v, error %v", stats, err)
	}
	detail := viewer.post("/cairn.v1.ActivityService/GetActivity", map[string]any{"id": id.String()}, http.StatusOK)
	fields, ok := detail["activity"].(map[string]any)
	summary, _ := fields["summary"].(map[string]any)
	if !ok || fields["elapsedDuration"] != "400s" || fields["movingDuration"] != "310s" || summary["avgSpeedMps"] != average {
		t.Fatalf("Connect detail: %+v", detail)
	}

	// A historical archive can still provide timing when the stored track
	// is missing. The backfill reads the file without restoring stream rows.
	archive := recordArchiveFixture{key: "raw/historical.gpx", raw: raw}
	if err := env.app.Activities.SetSourceRawBlob(ctx, sourceID, archive.key, fileimport.ContentType(".gpx"), int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	if err := env.app.Streams.DeleteStream(ctx, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx, "UPDATE activities SET moving_duration_s = 400, tss = 50 WHERE id = $1", id.UUID()); err != nil {
		t.Fatal(err)
	}
	if count, err := backfillMovingTime(ctx, env.pool, activityTimingArchive{blobs: archive}); err != nil || count != 1 {
		t.Fatalf("GPX archive backfill changed %d activities, error %v", count, err)
	}
	if current, err := env.app.Activities.GetActivity(ctx, id); err != nil || current.MovingDuration != act.MovingDuration {
		t.Fatalf("GPX archive moving time = %s, error %v", current.MovingDuration, err)
	}
	if empty, err := env.app.Streams.QueryStream(ctx, query); err != nil || len(empty.Samples) != 0 {
		t.Fatalf("GPX backfill rewrote the empty stream, error %v", err)
	}
}

func TestAPI_HistoricalFITMovingTime(t *testing.T) {
	env := newITestEnv(t)
	rider := env.user("fit-timing", domain.UserRoleUser)
	ctx := t.Context()
	start := time.Date(2024, 8, 12, 9, 0, 0, 0, time.UTC)
	original := domain.Activity{
		Type: domain.ActivityTypeRide, StartTime: start, EndTime: start.Add(210 * time.Second),
		ElapsedDuration: 210 * time.Second, MovingDuration: 155 * time.Second,
		Summary: domain.ActivitySummary{DistanceM: new(1050.0)},
	}
	fit, err := export.FIT(original, domain.Stream{})
	if err != nil {
		t.Fatal(err)
	}
	archive := recordArchiveFixture{key: "raw/historical.fit", raw: fit}
	out, err := env.app.IngestActivity.Execute(ctx, activity.IngestInput{
		UserID: rider.userID, Provider: "upload", ExternalID: "historical-fit",
		SourceWorkerName: "file-upload", SourceWorkerVersion: "1.0", RawBlobID: archive.key,
		Payload: domain.ActivitySourcePayload{
			Type: original.Type, StartTime: start, EndTime: original.EndTime, Timezone: "UTC",
			ElapsedDuration: original.ElapsedDuration, MovingDuration: original.ElapsedDuration, Summary: original.Summary,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		archive     port.ActivityTimingArchive
		wantChanges int
		wantMoving  time.Duration
	}{
		{"missing measurements", nil, 0, original.ElapsedDuration},
		{"timer recovered", activityTimingArchive{blobs: archive}, 1, original.MovingDuration},
		{"repeat", activityTimingArchive{blobs: archive}, 0, original.MovingDuration},
	} {
		n, err := backfillMovingTime(ctx, env.pool, test.archive)
		if err != nil || n != test.wantChanges {
			t.Fatalf("%s changed %d rows, want %d, error %v", test.name, n, test.wantChanges, err)
		}
		act, err := env.app.Activities.GetActivity(ctx, out.ActivityID)
		if err != nil || act.MovingDuration != test.wantMoving {
			t.Fatalf("%s timing: %s, error %v", test.name, act.MovingDuration, err)
		}
	}
	source, err := env.app.Activities.GetSource(ctx, out.SourceID)
	if err != nil || source.Parsed.MovingDuration != original.ElapsedDuration || source.RawBlobID != archive.key {
		t.Fatalf("archived source was modified: %+v, error %v", source, err)
	}
}
