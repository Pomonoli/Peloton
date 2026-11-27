//go:build integration

package postgres

import (
	"testing"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	routecatalog "github.com/opencairn-org/cairn-core/internal/usecase/routes"
)

func TestRecordedRoutesMapUsesFavoriteCatalogFilters(t *testing.T) {
	ctx, pool := requirePool(t)
	ada, mira := seedUser(t), seedUser(t)
	repo := NewRecordedRouteRepo(pool)
	catalog := &routecatalog.Catalog{Repo: repo, Zones: NewVisibilityRepo(pool)}
	track := domain.RecordedTrack{{
		{Lat: 51, Lon: 4}, {Lat: 51, Lon: 4.001}, {Lat: 51, Lon: 4.004},
	}}
	addTrack := func(rider domain.UserID, distance float64, line domain.RecordedTrack) domain.ActivityID {
		t.Helper()
		var id domain.ActivityID
		if err := pool.QueryRow(ctx, `INSERT INTO activities
			(user_id,type,start_time,end_time,elapsed_duration_s,moving_duration_s,distance_m)
			VALUES ($1,'ride',now()-interval '1 hour',now(),3600,3600,$2) RETURNING id`,
			rider.UUID(), distance).Scan(&id); err != nil {
			t.Fatal(err)
		}
		var source domain.SourceID
		if err := pool.QueryRow(ctx, `INSERT INTO activity_sources
			(activity_id,user_id,provider,external_id,source_worker_name,source_worker_version,source_worker_package,parsed)
			VALUES ($1,$2,'upload',$1::uuid::text,'test','1','test','{"has_stream":true}') RETURNING id`,
			id.UUID(), rider.UUID()).Scan(&source); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE activities SET primary_stream_source_id=$1 WHERE id=$2`, source.UUID(), id.UUID()); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveTrack(ctx, port.RouteTrack{Activity: id, Source: source, LengthM: distance, Kind: domain.RecordedLoop, Track: line}); err != nil {
			t.Fatal(err)
		}
		return id
	}

	addFavorite := func(rider domain.UserID, distance float64, line domain.RecordedTrack) domain.RecordedRouteID {
		t.Helper()
		a := addTrack(rider, distance, line)
		b := addTrack(rider, distance, line)
		if err := repo.SaveCluster(ctx, nil, a, []port.RouteMatch{{Activity: a, Overlap: 1}, {Activity: b, Overlap: 1}}); err != nil {
			t.Fatal(err)
		}
		id, err := repo.Membership(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM recorded_routes WHERE id=$1", id.UUID()) })
		return id
	}
	first := addFavorite(ada, 24_000, track)
	shifted := domain.RecordedTrack{{
		{Lat: 51.00002, Lon: 4}, {Lat: 51.00002, Lon: 4.004},
	}}
	second := addFavorite(mira, 56_000, shifted)
	miraUser, err := NewUserRepo(pool).GetUser(ctx, mira)
	if err != nil {
		t.Fatal(err)
	}
	load := func(q port.RecordedRouteQuery) port.RecordedRouteListing {
		t.Helper()
		out, err := catalog.List(ctx, ada, q)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	list := load(port.RecordedRouteQuery{Limit: 1, Search: "loop"})
	all := load(port.RecordedRouteQuery{Map: true, Sort: "distance", Limit: 1, Search: "loop"})
	if len(list.Routes) != 1 || len(all.Routes) < 2 || list.Total != all.Total {
		t.Fatalf("list page and map disagree: total=%d/%d, rows=%d/%d", list.Total, all.Total, len(list.Routes), len(all.Routes))
	}
	var tracks []domain.RecordedTrack
	for _, item := range all.Routes {
		if item.ID == first || item.ID == second {
			tracks = append(tracks, item.Track)
		}
	}
	if len(tracks) != 2 {
		t.Fatal("map catalog is missing a favorite")
	}
	lines, err := domain.RecordedRouteMap(ctx, tracks)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || len(lines[0].Routes) != 2 || len(lines[1].Routes) != 2 {
		t.Fatalf("two favorites from different riders should both match: %+v", lines)
	}
	filter := port.RecordedRouteQuery{Rider: miraUser.Username, MinDistanceM: 40_000, MaxDistanceM: 80_000, Limit: 12}
	for _, mapView := range []bool{false, true} {
		filter.Map = mapView
		got := load(filter)
		if got.Total != 1 || len(got.Routes) != 1 || got.Routes[0].ID != second || got.Routes[0].Rides != 2 ||
			len(got.Routes[0].Riders) != 1 || got.Routes[0].Riders[0].UserID != mira {
			t.Fatalf("map=%t: unexpected filtered catalog %+v", mapView, got)
		}
		filtered, err := domain.RecordedRouteMap(ctx, []domain.RecordedTrack{got.Routes[0].Track})
		if err != nil {
			t.Fatal(err)
		}
		if len(filtered) != 1 || len(filtered[0].Routes) != 1 {
			t.Fatalf("filter must update density: %+v", filtered)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO privacy_zones (user_id,lat,lng,radius_m)
		VALUES ($1,51,4.002,250)`, mira.UUID()); err != nil {
		t.Fatal(err)
	}
	filter.Map = true
	masked := load(filter)
	if len(masked.Routes) != 1 || masked.Routes[0].ID != second || len(masked.Routes[0].Track) != 0 {
		t.Fatal("another rider's privacy zone must mask the track while retaining the catalog entry")
	}

}
