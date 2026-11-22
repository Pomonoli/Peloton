//go:build integration

package postgres

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/heatmap"
	"github.com/opencairn-org/cairn-core/internal/port"
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
}
