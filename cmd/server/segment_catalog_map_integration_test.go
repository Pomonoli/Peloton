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

func TestAPI_SegmentCatalogPagesAcrossRiders(t *testing.T) {
	env := newITestEnv(t)
	ctx := t.Context()
	alice := env.user("alice", domain.UserRoleUser)
	bob := env.user("bob", domain.UserRoleUser)
	carol := env.user("carol", domain.UserRoleUser)
	start := time.Date(2025, 6, 12, 12, 0, 0, 0, time.UTC)
	const segmentCount = 240
	if _, err := env.pool.Exec(ctx, `
		INSERT INTO segments (source, external_id, catalog_provider, scope, activity_type, name, polyline, polyline_precision, geom, bbox, distance_m)
		SELECT 'strava_catalog', i::text, 'test-map', 'instance', 'ride', 'Hill ' || lpad(i::text, 3, '0'),
		       'os|}G_ruo@?_|B?_|B', 5, ST_GeogFromText('LINESTRING(7.98 47, 8.02 47)'),
		       ST_MakeEnvelope(7.98, 46.999, 8.02, 47.001, 4326)::geography, 1200
		FROM generate_series(1, $1::int) i`, segmentCount); err != nil {
		t.Fatal(err)
	}
	for i, rider := range []*itestClient{alice, bob, carol} {
		ride := rider.upload("ride.gpx", gpxRide(start))
		activityID := str(ride, "activity_id")
		limit, attempts, elapsed := segmentCount, 60, 100-i*20
		if rider == alice {
			limit, attempts = 60, 2
		}
		if _, err := env.pool.Exec(ctx, `
			INSERT INTO segment_efforts (segment_id, activity_id, activity_source_id, user_id, start_time, elapsed_s, moving_s, start_offset, end_offset)
			SELECT s.id, a.id, $2::uuid, a.user_id, a.start_time + i * interval '1 minute',
			       $3::float8, $3::float8, i*60, i*60+1
			FROM segments s CROSS JOIN activities a CROSS JOIN generate_series(1, $4::int) i
			WHERE a.id = $1::uuid AND CAST(s.external_id AS int) <= $5::int`,
			activityID, str(ride, "source_id"), elapsed, attempts, limit); err != nil {
			t.Fatal(err)
		}
		if rider == carol {
			if _, err := env.pool.Exec(ctx, "UPDATE activities SET provider_restricted = true WHERE id = $1::uuid", activityID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := env.pool.Exec(ctx, "ANALYZE segment_efforts"); err != nil {
		t.Fatal(err)
	}

	alice.http.Timeout = 5 * time.Second
	const pageSize = 50
	for _, tc := range []struct {
		query string
		count int
	}{
		{"sort=name:asc", segmentCount},
		{"mine=true&sort=my_attempts:desc", 60},
		{"mine=true&member=bob&sort=my_attempts:desc", segmentCount},
	} {
		for offset := 0; offset < tc.count; offset += pageSize {
			path := fmt.Sprintf("/api/segments/catalog?%s&limit=%d&offset=%d", tc.query, pageSize, offset)
			begin := time.Now()
			var page struct {
				Segments []struct {
					Name     string
					Holder   string  `json:"local_kom_holder"`
					Elapsed  float64 `json:"best_elapsed_s"`
					Speed    float64 `json:"average_speed_kmh"`
					Members  int     `json:"member_count"`
					Attempts int     `json:"my_attempts"`
				}
				HasMore bool `json:"has_more"`
			}
			if err := json.Unmarshal(alice.do(http.MethodGet, path, nil, http.StatusOK), &page); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s took %s", path, time.Since(begin))
			if len(page.Segments) != min(pageSize, tc.count-offset) || page.HasMore != (offset+pageSize < tc.count) {
				t.Fatalf("%s returned %d rows (has_more %t)", path, len(page.Segments), page.HasMore)
			}
			for i, row := range page.Segments {
				number := offset + i + 1
				members, attempts := 1, 0
				if number <= 60 {
					members, attempts = 2, 2
				}
				if tc.query == "mine=true&member=bob&sort=my_attempts:desc" {
					attempts = 60
				}
				if row.Name != fmt.Sprintf("Hill %03d", number) || row.Holder != "Bob" || row.Elapsed != 80 ||
					row.Speed != 54 || row.Members != members || row.Attempts != attempts {
					t.Fatalf("%s: wrong row for hill %03d: %+v", path, number, row)
				}
			}
		}
	}
}

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
