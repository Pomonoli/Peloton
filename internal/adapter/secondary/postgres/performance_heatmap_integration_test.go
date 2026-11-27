//go:build integration

package postgres

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/heatmap"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/dashboard"
)

func TestPerformanceHeatmapReadsMoreThan5000CachedRides(t *testing.T) {
	ctx, pool := requirePool(t)
	userID := seedUser(t)
	const count = 5001
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", userID.UUID()) })
	for _, statement := range []string{
		`INSERT INTO activities (user_id,type,start_time,end_time,elapsed_duration_s,moving_duration_s)
		 SELECT $1,'ride',now()-i*interval '1 day',now()-i*interval '1 day'+interval '1 hour',3600,3600
		 FROM generate_series(1,5001) i`,
		`INSERT INTO activity_sources (activity_id,user_id,provider,external_id,source_worker_name,source_worker_version,source_worker_package,parsed)
		 SELECT id,user_id,'upload',id::text,'test','1','test','{"has_stream":true}' FROM activities WHERE user_id=$1`,
		`UPDATE activities a SET primary_stream_source_id=src.id FROM activity_sources src
		 WHERE a.id=src.activity_id AND a.user_id=$1`,
		`INSERT INTO performance_heatmap_cache (activity_id,signature,has_track) SELECT a.id,` + heatmapSignatureSQL + `,true` +
			heatmapSourcesSQL + ` WHERE a.user_id=$1 GROUP BY a.id,p.id`,
	} {
		if _, err := pool.Exec(ctx, statement, userID.UUID()); err != nil {
			t.Fatal(err)
		}
	}
	bin := heatmap.Bin{Key: heatmap.Key{Along: 1, Across: 1}, Passages: 1, Distance: 20, Moving: 3}
	payload, err := binary.Append(nil, binary.LittleEndian, []heatmap.Bin{bin})
	if err != nil {
		t.Fatal(err)
	}
	tile := bin.Key.Tile(heatmap.Detail)
	if _, err := pool.Exec(ctx, `INSERT INTO performance_heatmap_tiles (activity_id,level,tile_x,tile_y,bins)
		SELECT id,2,$2,$3,$4 FROM activities WHERE user_id=$1`, userID.UUID(), int16(tile.X), int16(tile.Y), payload); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	repo := NewPerformanceHeatmapRepo(pool)
	activities, err := repo.HeatmapActivities(ctx, userID, port.ActivityListFilter{Type: string(domain.ActivityTypeRide)})
	if err != nil || len(activities) != count {
		t.Fatalf("cached history length = %d, error %v", len(activities), err)
	}
	var total heatmap.Bin
	viewport := port.HeatmapViewport{Level: heatmap.Detail, Bounds: &domain.CatalogSegmentBounds{MinLon: -0.01, MaxLon: 0.01, MinLat: -0.01, MaxLat: 0.01}}
	if err := repo.VisitHeatmapBins(ctx, userID, activities, viewport, total.Merge); err != nil {
		t.Fatal(err)
	}
	if total.Passages != count || total.Distance != 20*count {
		t.Fatalf("history truncated: %+v", total)
	}
	t.Logf("selected and merged %d cached activities in %v", count, time.Since(start))
	start = time.Now()
	result, err := dashboard.NewPerformanceMaps(NewActivityRepo(pool), repo).Discover(ctx, userID,
		[]domain.UserID{userID}, domain.UserID{}, port.ActivityListFilter{Limit: 25}, port.HeatmapCell{Level: heatmap.Detail, Key: bin.Key})
	if err != nil || result.Matched != count || result.Passages != count || len(result.Activities) != 25 {
		t.Fatalf("discovery result: matched=%d passages=%d rows=%d err=%v", result.Matched, result.Passages, len(result.Activities), err)
	}
	t.Logf("found %d contributors and read first page in %v", result.Matched, time.Since(start))
}

func TestPerformanceHeatmapDiscoveryMatchesDisplayedContributions(t *testing.T) {
	ctx, pool := requirePool(t)
	ada, mira := seedUser(t), seedUser(t)
	viewer := ada
	for _, rider := range []domain.UserID{ada, mira} {
		t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", rider.UUID()) })
	}
	key := heatmap.Key{Along: 1, Across: 1}
	otherKey := heatmap.Key{Along: 2, Across: 1}
	from := time.Date(2025, 5, 12, 0, 0, 0, 0, time.UTC)

	addRide := func(rider domain.UserID, title string, date time.Time, hidden, restricted bool, passages uint32) domain.ActivityID {
		t.Helper()
		var id domain.ActivityID
		err := pool.QueryRow(ctx, `INSERT INTO activities
			(user_id,type,title,start_time,end_time,elapsed_duration_s,moving_duration_s,hidden_by_admin,provider_restricted)
			VALUES ($1,'ride',$2,$3,$3::timestamptz+interval '1 hour',3600,3600,$4,$5) RETURNING id`,
			rider.UUID(), title, date, hidden, restricted).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			`INSERT INTO activity_sources (activity_id,user_id,provider,external_id,source_worker_name,source_worker_version,source_worker_package,parsed)
			 SELECT id,user_id,'upload',id::text,'test','1','test','{"has_stream":true}' FROM activities WHERE id=$1`,
			`UPDATE activities a SET primary_stream_source_id=src.id FROM activity_sources src
			 WHERE a.id=src.activity_id AND a.id=$1`,
		} {
			if _, err := pool.Exec(ctx, statement, id.UUID()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.Exec(ctx, `INSERT INTO performance_heatmap_cache(activity_id,signature,has_track) SELECT a.id,`+
			heatmapSignatureSQL+`,true`+heatmapSourcesSQL+` WHERE a.id=$1 GROUP BY a.id,p.id`, id.UUID()); err != nil {
			t.Fatal(err)
		}
		payload, err := binary.Append(nil, binary.LittleEndian, []heatmap.Bin{
			{Key: key, Passages: passages, Distance: 20, Moving: 3},
			{Key: otherKey, Passages: 100, Distance: 20, Moving: 3},
		})
		if err != nil {
			t.Fatal(err)
		}
		tile := key.Tile(heatmap.Detail)
		if _, err := pool.Exec(ctx, `INSERT INTO performance_heatmap_tiles(activity_id,level,tile_x,tile_y,bins)
			VALUES ($1,2,$2,$3,$4)`, id.UUID(), int16(tile.X), int16(tile.Y), payload); err != nil {
			t.Fatal(err)
		}
		return id
	}

	repeated := addRide(ada, "Repeated passage", from, false, false, 3)
	other := addRide(mira, "Single passage", from.Add(time.Hour), false, false, 1)
	addRide(mira, "Out of range", from.Add(-time.Hour), false, false, 10)
	addRide(mira, "Moderated", from, true, false, 10)
	addRide(mira, "Restricted", from, false, true, 10)

	maps := dashboard.NewPerformanceMaps(NewActivityRepo(pool), NewPerformanceHeatmapRepo(pool))
	filter := port.ActivityListFilter{From: from, Limit: 1, Sort: "date_desc"}
	members := []domain.UserID{ada, mira}
	cell := port.HeatmapCell{Key: key, Level: heatmap.Detail}
	request := func(selected domain.UserID) dashboard.HeatmapDiscovery {
		t.Helper()
		got, err := maps.Discover(ctx, viewer, members, selected, filter, cell)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	got := request(domain.UserID{})
	if got.Passages != 4 || got.Matched != 2 || len(got.Riders) != 2 ||
		got.Riders[0].Rider != ada || got.Riders[0].Passages != 3 || got.Riders[0].Activities != 1 ||
		len(got.Activities) != 1 || got.Activities[0].ID != other {
		t.Fatalf("unexpected discovery %+v", got)
	}
	got = request(ada)
	if got.Passages != 4 || got.Matched != 1 || len(got.Riders) != 2 ||
		len(got.Activities) != 1 || got.Activities[0].ID != repeated {
		t.Fatalf("unexpected rider filter %+v", got)
	}
	filter.Offset = 1
	got = request(domain.UserID{})
	if len(got.Activities) != 1 || got.Activities[0].ID != repeated {
		t.Fatalf("unexpected second page %+v", got)
	}
	filter.Offset = 0

	checkMap := func(want uint64) {
		t.Helper()
		displayed, err := maps.Get(ctx, viewer, members, filter, port.HeatmapViewport{Level: heatmap.Detail})
		if err != nil {
			t.Fatal(err)
		}
		for _, bin := range displayed.Bins {
			if bin.Key == key && uint64(bin.Passages) == want {
				return
			}
		}
		t.Fatalf("expected %d displayed passages in cell, got %+v", want, displayed.Bins)
	}
	checkMap(4)
	if _, err := pool.Exec(ctx, `INSERT INTO privacy_zones (user_id,lat,lng,radius_m)
		VALUES ($1,0,0,250)`, mira.UUID()); err != nil {
		t.Fatal(err)
	}
	got = request(domain.UserID{})
	if got.Passages != 3 || got.Matched != 1 || len(got.Riders) != 1 || got.Riders[0].Rider != ada {
		t.Fatalf("private contribution escaped masking %+v", got)
	}
	checkMap(3)
}
