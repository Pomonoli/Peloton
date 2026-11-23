//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

func TestAPI_SegmentCatalogSearchAndViewport(t *testing.T) {
	env := newITestEnv(t)
	alice := env.user("alice", domain.UserRoleUser)
	bob := env.user("bob", domain.UserRoleUser)
	start := time.Date(2025, 6, 12, 12, 0, 0, 0, time.UTC)
	type ride struct {
		owner  domain.UserID
		id     domain.ActivityID
		source domain.SourceID
	}
	rides := make([]ride, 0, 2)
	for _, rider := range []*itestClient{alice, bob} {
		upload := rider.upload("ride.gpx", gpxRide(start))
		id, err := domain.ParseUUID[domain.ActivityID](str(upload, "activity_id"))
		if err != nil {
			t.Fatal(err)
		}
		source, err := domain.ParseUUID[domain.SourceID](str(upload, "source_id"))
		if err != nil {
			t.Fatal(err)
		}
		rides = append(rides, ride{owner: rider.userID, id: id, source: source})
	}
	const crossing = "os|}G_ruo@?_|B?_|B" // starts west of the viewport and crosses it
	const dateline = "_c`|@_v_ia@?~l`scA"
	for i, fixture := range []struct {
		name, polyline string
		distance       float64
		times          [2]float64
	}{
		{"Alpine Ridge", crossing, 800, [2]float64{50, 85}},
		{"Corner Ridge", "g{z}G_ruo@od@_sN", 700, [2]float64{70, 98}}, // bbox overlaps but the route misses
		{"Percent% Ridge", "w`|}G_oyo@od@od@", 600, [2]float64{0, 60}},
		{"Remote Under_score Ridge", "_uhuH_qrYo}@_|B", 1200, [2]float64{122, 0}},
		{"Dateline Pass", dateline, 1500, [2]float64{0, 150}},
		{"Unridden Ridge", crossing, 800, [2]float64{}},
	} {
		externalID := fmt.Sprint(i + 1)
		provider := "test-map"
		seg := domain.Segment{
			ID: domain.SegmentID(uuid.New()), Source: domain.SegmentSourceStravaCatalog,
			ExternalID: &externalID, CatalogProvider: &provider, Scope: domain.SegmentScopeInstance,
			Name: fixture.name, ActivityType: domain.ActivityTypeRide, DistanceM: fixture.distance,
			Polyline: fixture.polyline, PolylinePrecision: 5,
		}
		if err := env.app.Segments.SaveSegment(t.Context(), seg); err != nil {
			t.Fatal(err)
		}
		for j, elapsed := range fixture.times {
			if elapsed == 0 {
				continue
			}
			ride := rides[j]
			if err := env.app.Segments.SaveSegmentEffort(t.Context(), domain.SegmentEffort{
				ID: domain.SegmentEffortID(uuid.New()), SegmentID: seg.ID, ActivityID: ride.id,
				ActivitySourceID: ride.source, UserID: ride.owner, StartTime: start,
				ElapsedS: elapsed, MovingS: elapsed, EndOffset: 1,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	type resultRow struct {
		ID         string
		Name       string
		Polyline   string
		Precision  int `json:"polyline_precision"`
		Start      [2]float64
		BestTime   float64 `json:"best_elapsed_s"`
		KOM        string  `json:"local_kom_holder"`
		Members    int     `json:"member_count"`
		MyAttempts int     `json:"my_attempts"`
	}
	catalog := func(c *itestClient, query string, more bool, names ...string) []resultRow {
		t.Helper()
		body := c.do(http.MethodGet, "/api/segments/catalog?"+query, nil, http.StatusOK)
		var result struct {
			Segments []resultRow
			HasMore  bool `json:"has_more"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		actual := make([]string, 0, len(result.Segments))
		for _, row := range result.Segments {
			actual = append(actual, row.Name)
		}
		if !slices.Equal(actual, names) || result.HasMore != more {
			t.Fatalf("%q: got %v (has_more %v), want %v (has_more %v)", query, actual, result.HasMore, names, more)
		}
		return result.Segments
	}
	catalog(alice, "limit=2", true, "Alpine Ridge", "Corner Ridge")
	found := catalog(alice, "q="+url.QueryEscape("  rEmOtE  "), false, "Remote Under_score Ridge")
	if found[0].Polyline != "" || found[0].Start != [2]float64{4.36, 50.84} {
		t.Fatalf("expected only the map starting point for a list row: %+v", found[0])
	}
	catalog(alice, "limit=1&offset=1&sort=distance_m&direction=desc&q=riDgE", true, "Alpine Ridge")
	catalog(alice, "q=%25", false, "Percent% Ridge")
	catalog(alice, "q=_", false, "Remote Under_score Ridge")
	catalog(bob, "q=rIDGe&mine=true", false, "Alpine Ridge", "Corner Ridge", "Percent% Ridge")

	const near = "min_lon=7.99&min_lat=47.005&max_lon=8.01&max_lat=47.015"
	catalog(bob, near+"&limit=1", true, "Alpine Ridge")
	results := catalog(bob, near+"&q=alPINE&mine=true", false, "Alpine Ridge")
	if row := results[0]; row.Polyline != crossing || row.Precision != 5 || row.BestTime != 50 || row.KOM != "Alice" || row.Members != 2 {
		t.Fatalf("wrong map route or community stats: %+v", row)
	}
	catalog(bob, near+"&q=ridge", false, "Alpine Ridge", "Percent% Ridge")
	catalog(alice, near+"&mine=true", false, "Alpine Ridge")
	wrapped := catalog(alice, "min_lon=179.95&min_lat=9.9&max_lon=-179.95&max_lat=10.1", false, "Dateline Pass")
	if wrapped[0].Polyline != dateline {
		t.Fatalf("wrong wrapped route: %+v", wrapped[0])
	}
	// Alpine has a higher numeric speed. Corner, Percent and Dateline all
	// average exactly 36 km/h, so time/distance sorts must operate across pages.
	const speed = "sort=average_speed_kmh:desc&sort=member_count:desc&sort=best_elapsed_s:asc"
	catalog(alice, speed+"&limit=2", true, "Alpine Ridge", "Corner Ridge")
	catalog(alice, speed+"&limit=2&offset=2", true, "Percent% Ridge", "Dateline Pass")
	catalog(alice, speed+"&limit=2&offset=4", false, "Remote Under_score Ridge")
	catalog(alice, speed+"&q=ridge&mine=true&member=bob", false, "Alpine Ridge", "Corner Ridge", "Percent% Ridge")
	catalog(alice, near+"&"+speed, false, "Alpine Ridge", "Percent% Ridge")
	catalog(alice, "sort=average_speed_kmh:asc&sort=distance_m:desc", false,
		"Remote Under_score Ridge", "Dateline Pass", "Corner Ridge", "Percent% Ridge", "Alpine Ridge")

	for _, query := range []string{"sort=member_count:down", "sort=unknown:asc", "sort=name:asc&sort=name:desc", "min_lon=7.99", "min_lon=NaN&min_lat=47&max_lon=8&max_lat=48",
		"min_lon=-180&min_lat=-90&max_lon=180&max_lat=90"} {
		alice.do(http.MethodGet, "/api/segments/catalog?"+query, nil, http.StatusBadRequest)
	}
	for i, id := range []string{"00000000-0000-0000-0000-000000000002", "00000000-0000-0000-0000-000000000001"} {
		externalID := fmt.Sprint(100 + i)
		segment := domain.Segment{
			ID: domain.SegmentID(uuid.MustParse(id)), Source: domain.SegmentSourceStravaCatalog,
			ExternalID: &externalID, CatalogProvider: new("test-map"), Scope: domain.SegmentScopeInstance,
			Name: "Exact tie", ActivityType: domain.ActivityTypeRide, DistanceM: 800, Polyline: crossing, PolylinePrecision: 5,
		}
		if err := env.app.Segments.SaveSegment(t.Context(), segment); err != nil {
			t.Fatal(err)
		}
		ride := rides[0]
		if err := env.app.Segments.SaveSegmentEffort(t.Context(), domain.SegmentEffort{
			ID: domain.SegmentEffortID(uuid.New()), SegmentID: segment.ID, ActivityID: ride.id, ActivitySourceID: ride.source,
			UserID: ride.owner, StartTime: start, ElapsedS: 80, MovingS: 80, EndOffset: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	tied := speed + "&q=exact+tie&limit=1"
	first := catalog(alice, tied, false, "Exact tie")
	if first[0].ID != "00000000-0000-0000-0000-000000000001" || first[0].MyAttempts != 1 {
		t.Fatalf("identical definitions or duplicate traversals were not deduplicated: %v", first)
	}

	// Two genuine traversals use distinct offsets within the same ride. A
	// rescan updates the natural key and a merged source repeats both passes.
	aliceRide := rides[0]
	sharedSource, err := env.app.Activities.GetSource(t.Context(), aliceRide.source)
	if err != nil {
		t.Fatal(err)
	}
	sharedSource.ID = domain.SourceID(uuid.New())
	sharedSource.ExternalID = "alternate-recording"
	if err := env.app.Activities.SaveSource(t.Context(), sharedSource); err != nil {
		t.Fatal(err)
	}
	target := domain.SegmentID(uuid.MustParse(first[0].ID))
	for _, sid := range []domain.SourceID{aliceRide.source, sharedSource.ID} {
		for _, offset := range []int{0, 120} {
			effort := domain.SegmentEffort{
				ID: domain.SegmentEffortID(uuid.New()), SegmentID: target, ActivityID: aliceRide.id, ActivitySourceID: sid,
				UserID: aliceRide.owner, StartTime: start.Add(time.Duration(offset) * time.Second), ElapsedS: 80,
				MovingS: 80, StartOffset: offset, EndOffset: offset + 80,
			}
			if err := env.app.Segments.SaveSegmentEffort(t.Context(), effort); err != nil {
				t.Fatal(err)
			}
			effort.ID = domain.SegmentEffortID(uuid.New())
			if err := env.app.Segments.SaveSegmentEffort(t.Context(), effort); err != nil {
				t.Fatal(err)
			}
		}
	}
	stack := "q=exact+tie&sort=my_attempts:desc&sort=best_elapsed_s:asc&limit=1"
	top := catalog(alice, stack, false, "Exact tie")[0]
	if top.ID != target.String() || top.MyAttempts != 2 {
		t.Fatalf("repeated attempts or deduplication failed: %+v", top)
	}
	peer := catalog(bob, "q=exact+tie&mine=true&member=alice&sort=my_attempts:desc", false, "Exact tie")
	if peer[0].MyAttempts != 2 {
		t.Fatal("attempts were not scoped to the rider being viewed")
	}
	// A legacy route without a start point used to fail scanning the whole page.
	if _, err := env.pool.Exec(t.Context(), "UPDATE segments SET geom = 'LINESTRING EMPTY'::geography WHERE id = $1", target.UUID()); err != nil {
		t.Fatal(err)
	}
	catalog(alice, "q=exact+tie", false, "Exact tie", "Exact tie")

	env.anon().do(http.MethodGet, "/api/segments/catalog?"+near, nil, http.StatusUnauthorized)
}
