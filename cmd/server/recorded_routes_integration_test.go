//go:build integration

package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/db"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
)

type catalogResult struct {
	Total  int
	Routes []struct {
		ID, Name, Type string
		Rides          int
		RiderCount     int     `json:"rider_count"`
		DistanceM      float64 `json:"distance_m"`
		BestSpeed      float64 `json:"best_speed_kmh"`
		Riders         []struct {
			Username    string
			DisplayName string `json:"display_name"`
			Rides       int
		}
	}
}

func TestAPI_RecordedRoutesLifecycle(t *testing.T) {
	env := newITestEnv(t)
	ada := env.user("ada", domain.UserRoleUser)
	mira := env.user("mira", domain.UserRoleUser)
	sam := env.user("sam", domain.UserRoleUser)
	ctx := t.Context()

	drain := func() {
		t.Helper()
		for range 100 {
			worked, err := env.app.RouteProcessor.ProcessOne(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !worked {
				return
			}
		}
		t.Fatal("route queue did not drain")
	}
	read := func(who *itestClient, query string) catalogResult {
		t.Helper()
		var out catalogResult
		routeJSON(t, who, "/api/routes"+query, &out)
		return out
	}
	add := func(who *itestClient, day, start int, reverse bool, speed, latitude float64) activity.IngestResult {
		t.Helper()
		from := time.Date(2025, 1, day, 9, 0, 0, 0, time.UTC)
		stream := routeLoop(from, start, reverse, speed, latitude)
		duration := stream[len(stream)-1].Timestamp.Sub(from)
		out, err := env.app.IngestActivity.Execute(ctx, activity.IngestInput{
			UserID: who.userID, Provider: "upload", SourceWorkerName: "test", ExternalID: fmt.Sprint(day),
			Payload: domain.ActivitySourcePayload{Type: domain.ActivityTypeRide, HasStream: true,
				StartTime: from, EndTime: from.Add(duration), Timezone: "UTC", MovingDuration: duration,
				ElapsedDuration: duration + 20*time.Minute, Summary: domain.ActivitySummary{
					DistanceM: new(4000.0), ElevationGainM: new(70.0),
				}},
			Stream: stream,
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	// A real upgrade must enqueue rides recorded before the route tables existed.
	if _, err := db.MigrateDownTo(ctx, env.pool, 89); err != nil {
		t.Fatal(err)
	}
	first := add(ada, 1, 0, false, 10, 51)
	add(mira, 2, 25, true, 11, 51)
	if _, err := db.MigrateUp(ctx, env.pool); err != nil {
		t.Fatal(err)
	}
	drain()
	env.anon().do(http.MethodGet, "/api/routes", nil, http.StatusUnauthorized)
	if out := read(ada, ""); out.Total != 0 {
		t.Fatalf("two riders incorrectly created a route: %+v", out)
	}

	adaSecond := add(ada, 3, 130, true, 10, 51)
	if err := env.app.Activities.SetStartLocation(ctx, first.ActivityID, new(51.0), new(4.0), "Waasland"); err != nil {
		t.Fatal(err)
	}
	drain()
	list := read(sam, "")
	if list.Total != 1 || len(list.Routes) != 1 {
		t.Fatalf("same-rider pair should create one route: %+v", list)
	}
	route := list.Routes[0]
	if route.Name != "Waasland Loop" || route.Rides != 3 || route.RiderCount != 2 ||
		route.Riders[0].Username != "ada" || route.Riders[0].Rides != 2 ||
		route.Type != "loop" || math.Abs(route.DistanceM-4000) > 10 || math.Abs(route.BestSpeed-39.6) > .3 {
		t.Fatalf("incorrect recorded route: %+v", route)
	}
	for _, query := range []string{"?rider=MIra&sort=rider_rides", "?unridden=true", "?q=waas&type=loop&min_km=3&max_km=5"} {
		if out := read(sam, query); out.Total != 1 || out.Routes[0].ID != route.ID {
			t.Fatalf("filter %s lost the route: %+v", query, out)
		}
	}
	if out := read(mira, "?unridden=true"); out.Total != 0 {
		t.Fatalf("ridden route appeared as unridden: %+v", out)
	}
	if out := read(sam, "?offset=100"); out.Total != 1 || len(out.Routes) != 0 {
		t.Fatalf("incorrect pagination: %+v", out)
	}

	miraSecond := add(mira, 4, 75, false, 9, 51)
	add(sam, 5, 215, false, 8, 51)
	add(sam, 6, 0, false, 8, 51.012)
	drain()
	if got := read(sam, "?rider=sam").Routes[0]; got.Rides != 5 || got.RiderCount != 3 || got.Riders[0].Username != "ada" {
		t.Fatalf("wrong historical memberships or tie-break: %+v", got)
	}
	miraThird := add(mira, 7, 0, false, 9, 51)
	add(mira, 7, 0, false, 9, 51) // duplicate provider/source
	drain()
	if got := read(sam, "").Routes[0]; got.ID != route.ID || got.Rides != 6 || got.Riders[0].Username != "mira" || got.Riders[0].Rides != 3 {
		t.Fatalf("incorrect featured rider after overtaking: %+v", got)
	}
	mira.rpc("/cairn.v1.AuthService/UpdateProfile", map[string]any{"displayName": "Mira Rider"}, http.StatusOK)
	if got := read(sam, "?rider=mira").Routes[0]; got.ID != route.ID || got.Riders[0].DisplayName != "Mira Rider" || got.Riders[0].Rides != 3 {
		t.Fatalf("recorded route after rider rename: %+v", got)
	}

	path := "/api/routes/" + route.ID
	var detail struct {
		Track  [][]struct{ Lat, Lon float64 }
		Recent []struct{ ID string }
	}
	routeJSON(t, sam, path, &detail)
	if len(detail.Track) != 1 || len(detail.Track[0]) < 100 || len(detail.Recent) != 5 || detail.Recent[0].ID != miraThird.ActivityID.String() {
		t.Fatalf("invalid route geometry or recent activity ordering: %+v", detail.Recent)
	}
	routeJSON(t, sam, path+"/activities?offset=5", &detail)
	if len(detail.Recent) != 1 || detail.Recent[0].ID != first.ActivityID.String() {
		t.Fatalf("incorrect recent activity pagination: %+v", detail.Recent)
	}
	var gpx struct {
		XMLName  xml.Name `xml:"http://www.topografix.com/GPX/1/1 gpx"`
		Name     string   `xml:"metadata>name"`
		Segments []struct {
			Points []struct {
				Lat float64 `xml:"lat,attr"`
				Lon float64 `xml:"lon,attr"`
			} `xml:"trkpt"`
		} `xml:"trk>trkseg"`
	}
	if err := xml.Unmarshal(sam.do(http.MethodGet, path+"/gpx", nil, http.StatusOK), &gpx); err != nil {
		t.Fatal(err)
	}
	if gpx.Name != route.Name || len(gpx.Segments) != 1 || len(gpx.Segments[0].Points) != len(detail.Track[0]) ||
		gpx.Segments[0].Points[0].Lat != 51 || gpx.Segments[0].Points[0].Lon != 4 {
		t.Fatalf("GPX did not export the representative recording")
	}
	if _, err := env.app.Visibility.AddPrivacyZone(ctx, domain.PrivacyZone{UserID: ada.userID, Lat: 51, Lng: 4, RadiusM: 150}); err != nil {
		t.Fatal(err)
	}
	routeJSON(t, sam, path, &detail)
	if len(detail.Track) == 0 || detail.Track[0][0].Lat == 51 && detail.Track[0][0].Lon == 4 {
		t.Fatal("detail exposes a private position")
	}
	if got := read(sam, "").Routes[0]; got.Name == "Waasland Loop" {
		t.Fatal("route name exposes a private start place")
	}
	sam.do(http.MethodGet, path+"/gpx", nil, http.StatusOK)

	// Modifying the original source must remove its stale membership and keep
	// the route ID while other riders still have a qualifying pair.
	if err := env.app.Streams.WriteStream(ctx, first.SourceID, routeLoop(time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC), 0, false, 10, 52)); err != nil {
		t.Fatal(err)
	}
	sam.do(http.MethodGet, path, nil, http.StatusNotFound)
	drain()
	if got := read(sam, ""); got.Total != 1 || got.Routes[0].ID != route.ID || got.Routes[0].Rides != 5 {
		t.Fatalf("replacing the anchor lost its route or counts: %+v", got)
	}
	for _, id := range []domain.ActivityID{miraSecond.ActivityID, miraThird.ActivityID} {
		if err := env.app.Activities.SoftDeleteActivity(ctx, id, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	sam.do(http.MethodGet, path, nil, http.StatusNotFound)
	drain()
	if out := read(sam, ""); out.Total != 0 {
		t.Fatalf("route without any remaining same-rider pair is still visible: %+v", out)
	}
	add(ada, 8, 220, false, 10, 51)
	drain()
	if _, err := env.app.RecomputeActivity.Execute(ctx, adaSecond.ActivityID); err != nil {
		t.Fatal(err)
	}
	drain()
	if got := read(sam, ""); got.Total != 1 || got.Routes[0].ID != route.ID || got.Routes[0].Rides != 4 {
		t.Fatalf("recompute inflated counts or changed the route ID: %+v", got)
	}
	var anchor domain.ActivityID
	if err := env.pool.QueryRow(ctx, `SELECT anchor_id FROM recorded_routes WHERE id = $1`, route.ID).Scan(&anchor); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE activities SET hidden_by_admin = true WHERE id = $1`, anchor.UUID()); err != nil {
		t.Fatal(err)
	}
	sam.do(http.MethodGet, path+"/gpx", nil, http.StatusNotFound)
	drain()
	if got := read(sam, ""); got.Total != 1 || got.Routes[0].ID != route.ID || got.Routes[0].Rides != 3 {
		t.Fatalf("moderated activity affected the shared route: %+v", got)
	}
	// Ada's rides in the less popular route are more frequent for Ada.
	add(mira, 9, 23, false, 10, 51.012)
	add(mira, 10, 100, true, 10, 51.012)
	add(ada, 11, 0, false, 10, 51.012)
	drain()
	popular := read(sam, "?rider=ada")
	personal := read(sam, "?rider=ada&sort=rider_rides")
	if popular.Total != 2 || popular.Routes[0].ID == route.ID ||
		personal.Total != 2 || personal.Routes[0].ID != route.ID {
		t.Fatalf("route ranking ignored the selected rider: default=%+v personal=%+v", popular, personal)
	}

}

func routeJSON(t *testing.T, rider *itestClient, path string, target any) {
	t.Helper()
	body := rider.do(http.MethodGet, path, nil, http.StatusOK)
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func routeLoop(start time.Time, offset int, reverse bool, speed, latitude float64) []domain.StreamSample {
	const steps = 400
	degrees := 180 / (math.Pi * domain.EarthRadiusMeters)
	if reverse {
		offset += steps
	}
	samples := make([]domain.StreamSample, steps+1)
	for i := range samples {
		j := (offset + i) % steps
		if reverse {
			j = (offset - i) % steps
		}
		along := float64(j%100) * 10
		var north, east float64
		switch j / 100 {
		case 0:
			north = along
		case 1:
			north, east = 1000, along
		case 2:
			north, east = 1000-along, 1000
		case 3:
			east = 1000 - along
		}
		samples[i] = domain.StreamSample{
			Timestamp: start.Add(time.Duration(1e9 * 10 * float64(i) / speed)),
			Latitude:  new(latitude + north*degrees), Longitude: new(4.0 + east*degrees/math.Cos(latitude*math.Pi/180)),
			AltitudeM: new(20.0 + north/20),
		}
	}
	return samples
}
