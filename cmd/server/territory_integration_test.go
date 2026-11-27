//go:build integration

package main

import (
	"encoding/json"
	"math"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/dashboard"
)

const territoryPath = "/api/activities/heatmap/territory"
const territoryLocation = territoryPath + "?lat=47.002&lon=8.009"

func TestAPI_TerritoryHeatmap(t *testing.T) {
	env := newITestEnv(t)
	alice := env.user("territory-alice", domain.UserRoleUser)
	bob := env.user("territory-bob", domain.UserRoleUser)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	upload := func(rider *itestClient, days int) map[string]any {
		return rider.upload("ride.gpx", performanceGPX(now.AddDate(0, 0, -days), time.Second, 145, 0))
	}
	first := upload(alice, 89)
	if again := upload(alice, 89); str(first, "activity_id") != str(again, "activity_id") {
		t.Fatal("repeated upload created another ride")
	}
	second := upload(alice, 88)
	upload(alice, 91)
	upload(bob, 3)

	env.anon().do(http.MethodGet, territoryPath, nil, http.StatusUnauthorized)
	env.anon().do(http.MethodGet, territoryLocation, nil, http.StatusUnauthorized)
	bob.do(http.MethodGet, territoryPath+"?lat=NaN&lon=8", nil, http.StatusBadRequest)
	bob.do(http.MethodGet, territoryPath+"?lon=8", nil, http.StatusBadRequest)

	var cold struct{ Pending int }
	body := bob.do(http.MethodGet, territoryPath, nil, http.StatusAccepted)
	if err := json.Unmarshal(body, &cold); err != nil || cold.Pending != 3 {
		t.Fatalf("expected three eligible rides to backfill: %s, %v", body, err)
	}
	var before dashboard.TerritoryMap
	routeJSON(t, bob, territoryPath+"?member=ignored&from=2000-01-01", &before)
	if len(before.GeoJSON.Features) != 1 || len(before.Legend) != 2 {
		t.Fatalf("expected one owner and two riders: %+v", before.Legend)
	}
	leader := before.Legend[0]
	if leader.ID != alice.userID.String() || leader.Cells < 2 || leader.AreaKm2 <= 0 || leader.Percentage != 100 ||
		before.Legend[1].Cells != 0 || before.Legend[1].Color == leader.Color {
		t.Fatalf("invalid territory legend: %+v", before.Legend)
	}
	polygons := before.GeoJSON.Features[0].Geometry.Coordinates
	if len(polygons) != 1 || len(polygons[0]) != 1 || len(polygons[0][0]) <= 7 {
		t.Fatalf("expected a merged territory without internal borders: %+v", polygons)
	}

	type cellResponse struct {
		OwnerID   string `json:"owner_id"`
		Distances []dashboard.TerritoryDistance
	}
	readCell := func(rider *itestClient) cellResponse {
		t.Helper()
		var out cellResponse
		routeJSON(t, rider, territoryLocation, &out)
		return out
	}
	cell := readCell(bob)
	if cell.OwnerID != alice.userID.String() || len(cell.Distances) != 2 ||
		math.Abs(cell.Distances[0].DistanceM-2*cell.Distances[1].DistanceM) > .002 {
		t.Fatalf("duplicate or expired ride affected the distance ranking: %+v", cell)
	}
	bob.do(http.MethodGet, territoryPath+"?lat=0&lon=0", nil, http.StatusNotFound)

	checkETag := func(version string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.srv.URL+territoryPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("If-None-Match", `"`+version+`"`)
		res, err := bob.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("conditional territory request: got %d, want %d", res.StatusCode, want)
		}
	}
	checkETag(before.Version, http.StatusNotModified)

	// Cross the cutoff without editing the activity or advancing the database clock.
	future, err := env.app.TerritoryMaps.Get(ctx, bob.userID, now.AddDate(0, 0, 3))
	if err != nil {
		t.Fatal(err)
	}
	if future.Legend[0].ID != bob.userID.String() || future.Legend[1].Cells != 0 {
		t.Fatalf("territory did not expire: %+v", future.Legend)
	}

	upload(bob, 2)
	thirdBob := upload(bob, 1)
	bob.do(http.MethodGet, territoryPath, nil, http.StatusAccepted)
	if updated := readCell(bob); updated.OwnerID != bob.userID.String() || len(updated.Distances) != 2 ||
		math.Abs(updated.Distances[0].DistanceM-3*cell.Distances[1].DistanceM) > .003 {
		t.Fatalf("new activity did not update ownership: %+v", updated)
	}
	admin := env.user("territory-admin", domain.UserRoleAdmin)
	admin.post("/api/admin/activities/"+str(thirdBob, "activity_id")+"/hide", map[string]any{"hidden": true}, http.StatusOK)
	restricted := upload(bob, 4)
	if _, err := env.pool.Exec(ctx, `UPDATE activities SET provider_restricted=true WHERE id=$1::text::uuid`, str(restricted, "activity_id")); err != nil {
		t.Fatal(err)
	}
	moderated := readCell(bob)
	if len(moderated.Distances) != 2 || moderated.Distances[0].DistanceM != moderated.Distances[1].DistanceM ||
		moderated.OwnerID != min(alice.userID.String(), bob.userID.String()) {
		t.Fatalf("hidden or restricted ride counted, or tie was unstable: %+v", moderated)
	}
	checkETag(before.Version, http.StatusOK)

	bob.post("/api/profile/privacy-zones", map[string]any{"lat": 47.002, "lng": 8.009, "radius_m": 50}, http.StatusOK)
	masked := readCell(bob)
	if !reflect.DeepEqual(masked, readCell(alice)) || masked.OwnerID != alice.userID.String() || len(masked.Distances) != 1 {
		t.Fatalf("privacy masking differed by viewer or lost the next owner: %+v", masked)
	}

	source, err := domain.ParseUUID[domain.SourceID](str(second, "source_id"))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := env.app.Streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: source})
	if err != nil {
		t.Fatal(err)
	}
	for i := range stream.Samples {
		stream.Samples[i].Latitude = new(47.08)
	}
	if err := env.app.Streams.WriteStream(ctx, source, stream.Samples); err != nil {
		t.Fatal(err)
	}
	bob.do(http.MethodGet, territoryPath, nil, http.StatusAccepted)
	revised := readCell(bob)
	if len(revised.Distances) != 1 || revised.Distances[0].DistanceM != cell.Distances[1].DistanceM {
		t.Fatalf("changed stream left old distances cached: %+v", revised)
	}

	alice.post("/api/profile/privacy-zones", map[string]any{"lat": 47.002, "lng": 8.009, "radius_m": 50}, http.StatusOK)
	bob.do(http.MethodGet, territoryLocation, nil, http.StatusNotFound)
}
