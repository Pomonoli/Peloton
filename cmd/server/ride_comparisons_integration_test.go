//go:build integration

package main

import (
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/db"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

func TestAPI_SharedActivitiesLifecycle(t *testing.T) {
	env := newITestEnv(t)
	ada := env.user("shared-ada", domain.UserRoleUser)
	mira := env.user("shared-mira", domain.UserRoleUser)
	viewer := env.user("shared-viewer", domain.UserRoleUser)
	ctx := t.Context()
	firstDay := time.Date(2021, 5, 8, 9, 0, 0, 0, time.UTC)
	secondDay := firstDay.AddDate(0, 0, 1)

	if _, err := db.MigrateDownTo(ctx, env.pool, 92); err != nil {
		t.Fatal(err)
	}
	aTrack := comparisonTestTrack(firstDay, 0, 600, 1, 7, 0, 51)
	a := ingestRecordedRide(t, ada, aTrack, 4200, 25)
	b := ingestRecordedRide(t, mira, comparisonTestTrack(firstDay, 80, 500, 4, 7, 0, 51.00015), 2940, 25)
	if dup := ingestRecordedRide(t, mira, comparisonTestTrack(firstDay, 80, 500, 4, 7, 0, 51.00015), 2940, 25); dup.ActivityID != b.ActivityID {
		t.Fatal("reimport did not retain the original activity")
	}
	if _, err := db.MigrateUp(ctx, env.pool); err != nil {
		t.Fatal(err)
	}

	path := fmt.Sprintf("/api/head-to-head/shared?first=%s&second=%s", ada.name, mira.name)
	reversed := fmt.Sprintf("/api/head-to-head/shared?first=%s&second=%s", mira.name, ada.name)
	read := func(client *itestClient, path string) view.SharedActivities {
		t.Helper()
		var out view.SharedActivities
		routeJSON(t, client, path, &out)
		return out
	}
	env.anon().do(http.MethodGet, path, nil, http.StatusUnauthorized)
	ada.do(http.MethodGet, "/api/head-to-head/shared?second="+ada.name, nil, http.StatusBadRequest)
	ada.do(http.MethodGet, path+"&from=2021-05-10&to=2021-05-01", nil, http.StatusBadRequest)
	out := read(viewer, path)
	if len(out.Rides) != 1 || len(out.Encounters) != 0 || math.Abs(out.SharedDistanceM-2940) > 5 ||
		out.Rides[0].DurationS != 420 || out.Rides[0].Activities[0].ID != a.ActivityID.String() ||
		out.Rides[0].Activities[1].ID != b.ActivityID.String() {
		t.Fatalf("historical shared ride: %+v", out)
	}
	var signature string
	cacheVersion := func() string {
		t.Helper()
		var value string
		if err := env.pool.QueryRow(ctx, "SELECT xmin::text || signature FROM shared_ride_cache WHERE visibility = 0").Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	signature = cacheVersion()
	swap := read(viewer, reversed)
	if len(swap.Rides) != 1 || swap.SharedDistanceM != out.SharedDistanceM ||
		swap.Rides[0].Activities[0] != out.Rides[0].Activities[1] || cacheVersion() != signature {
		t.Fatalf("reversing riders changed the detection or cache: %+v", swap)
	}

	ingestRecordedRide(t, ada, comparisonTestTrack(secondDay, 0, 120, 1, 7, -420, 51), 840, 25)
	c := ingestRecordedRide(t, mira, comparisonTestTrack(secondDay, 2, 122, 4, -6, 360, 51.00004), 720, 25)
	onDay := path + "&from=2021-05-09&to=2021-05-09"
	out = read(viewer, onDay)
	if len(out.Rides) != 0 || len(out.Encounters) != 1 ||
		out.Encounters[0].Activities[1].ID != c.ActivityID.String() ||
		math.Abs(out.Encounters[0].Time.Sub(secondDay.Add(time.Minute)).Seconds()) > .1 {
		t.Fatalf("new ride date filter or crossing: %+v", out)
	}
	zone, err := env.app.Visibility.AddPrivacyZone(ctx, domain.PrivacyZone{
		UserID: mira.userID, Lat: 51.00004, Lng: 4, RadiusM: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out = read(viewer, onDay); len(out.Encounters) != 0 {
		t.Fatalf("new privacy zone did not mask cached encounter: %+v", out)
	}
	if out = read(mira, onDay); len(out.Encounters) != 1 {
		t.Fatalf("owner cannot see their own unmasked activity: %+v", out)
	}
	if err := env.app.Visibility.DeletePrivacyZone(ctx, zone, mira.userID); err != nil {
		t.Fatal(err)
	}
	ride, err := env.app.Activities.GetActivity(ctx, c.ActivityID)
	if err != nil {
		t.Fatal(err)
	}
	ride.ProviderRestricted = true
	if err := env.app.Activities.SaveActivity(ctx, ride); err != nil {
		t.Fatal(err)
	}
	if out = read(viewer, onDay); len(out.Encounters) != 0 {
		t.Fatalf("provider-restricted ride exposed through cached encounter: %+v", out)
	}
	ride.ProviderRestricted = false
	if err := env.app.Activities.SaveActivity(ctx, ride); err != nil {
		t.Fatal(err)
	}
	if err := env.app.Moderation.SetActivityHidden(ctx, c.ActivityID, true); err != nil {
		t.Fatal(err)
	}
	if out = read(viewer, onDay); len(out.Encounters) != 0 {
		t.Fatalf("moderated ride exposed through cached encounter: %+v", out)
	}
	if err := env.app.Moderation.SetActivityHidden(ctx, c.ActivityID, false); err != nil {
		t.Fatal(err)
	}
	if err := env.app.Streams.WriteStream(ctx, b.SourceID, comparisonTestTrack(firstDay, 80, 500, 4, 7, 0, 51.08)); err != nil {
		t.Fatal(err)
	}
	if out = read(viewer, path); len(out.Rides) != 0 || len(out.Encounters) != 1 {
		t.Fatalf("stream revision did not invalidate the ride: %+v", out)
	}
	if err := env.app.Activities.SoftDeleteActivity(ctx, c.ActivityID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if out = read(viewer, path); len(out.Rides) != 0 || len(out.Encounters) != 0 {
		t.Fatalf("deletion did not remove cached result: %+v", out)
	}
}

func comparisonTestTrack(day time.Time, from, to, step int, speed, origin, latitude float64) []domain.StreamSample {
	var out []domain.StreamSample
	for seconds := from; seconds <= to; seconds += step {
		longitude := 4 + (origin+float64(seconds)*speed)*180/(math.Pi*domain.EarthRadiusMeters*math.Cos(latitude*math.Pi/180))
		out = append(out, domain.StreamSample{
			Timestamp: day.Add(time.Duration(seconds) * time.Second), Latitude: &latitude, Longitude: &longitude,
		})
	}
	return out
}

func TestAPI_SpeedDistance(t *testing.T) {
	env := newITestEnv(t)
	ada := env.user("stats-ada", domain.UserRoleUser)
	mira := env.user("stats-mira", domain.UserRoleUser)
	add := func(c *itestClient, day int, typ domain.ActivityType, distance float64, moving time.Duration) domain.ActivityID {
		t.Helper()
		start := time.Date(2024, 8, day, 9, 0, 0, 0, time.UTC)
		result, err := env.app.IngestActivity.Execute(t.Context(), activity.IngestInput{
			UserID: c.userID, Provider: "manual", SourceWorkerName: "test", ExternalID: start.String(),
			Payload: domain.ActivitySourcePayload{
				Type: typ, Title: fmt.Sprint("Activity ", day), StartTime: start, EndTime: start.Add(time.Hour), Timezone: "UTC",
				ElapsedDuration: time.Hour, MovingDuration: moving, Summary: domain.ActivitySummary{DistanceM: &distance},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return result.ActivityID
	}
	a := add(ada, 1, domain.ActivityTypeRide, 30_000, time.Hour)
	b := add(mira, 1, domain.ActivityTypeRide, 15_000, 30*time.Minute)
	c := add(mira, 2, domain.ActivityTypeRide, 6000, 15*time.Minute)
	hidden := add(mira, 3, domain.ActivityTypeRide, 3500, 10*time.Minute)
	add(mira, 4, domain.ActivityTypeRun, 5000, 20*time.Minute)
	add(mira, 5, domain.ActivityTypeRide, 80_000, time.Minute)
	add(mira, 6, domain.ActivityTypeRide, 5000, 0)
	if _, err := env.pool.Exec(t.Context(), "UPDATE activities SET hidden_by_admin=true WHERE id=$1", hidden.UUID()); err != nil {
		t.Fatal(err)
	}
	type point struct {
		ID, Title string
		Distance  float64 `json:"distance_km"`
		Speed     float64 `json:"speed_kmh"`
	}
	const path = "/api/stats/speed-distance"
	read := func(who *itestClient, query string) []point {
		t.Helper()
		var points []point
		routeJSON(t, who, path+query, &points)
		return points
	}
	env.anon().do(http.MethodGet, path, nil, http.StatusUnauthorized)
	if got := read(ada, ""); len(got) != 1 || got[0].ID != a.String() || math.Abs(got[0].Speed-30) > .001 {
		t.Fatalf("unexpected self points: %+v", got)
	}
	if got := read(ada, "?member="+mira.name); len(got) != 2 || got[0].ID != b.String() || got[1].ID != c.String() ||
		got[0].Distance != 15 || math.Abs(got[0].Speed-30) > .001 || got[0].Title != "Activity 1" {
		t.Fatalf("unexpected community points: %+v", got)
	}
	if got := read(ada, "?member="+mira.name+"&from=2024-08-02&to=2024-08-03&distance_max_m=10000"); len(got) != 1 || got[0].ID != c.String() {
		t.Fatalf("date or distance filter ignored: %+v", got)
	}
	if got := read(ada, "?member="+mira.name+"&year=2024"); len(got) != 2 ||
		len(read(ada, "?member="+mira.name+"&year=2025")) != 0 {
		t.Fatalf("Stats year filter ignored: %+v", got)
	}
	if got := read(ada, "?member="+mira.name+"&type=run"); len(got) != 0 {
		t.Fatalf("non-cycling filter returned rides: %+v", got)
	}
}
