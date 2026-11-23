package postgres

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

type similarCandidate struct {
	domain.SimilarActivity
	key routeVersion
}

type routeVersion struct {
	source       domain.SourceID
	revision     int64
	activityType domain.ActivityType
}

type similarRouteCache struct {
	mu      sync.Mutex
	entries map[routeVersion]domain.RouteCoverage
	order   []routeVersion
	cost    int
}

// Bound both entry count and stored geometry. Large recordings can still
// be compared, and only serve that request if they exceed the cache budget.
func (c *similarRouteCache) save(key routeVersion, route domain.RouteCoverage) {
	const maxParts = 120_000
	const maxEntries = 128
	if route.Size() > maxParts {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[routeVersion]domain.RouteCoverage)
	}
	if _, exists := c.entries[key]; exists {
		return
	}
	for len(c.order) >= maxEntries || c.cost+route.Size() > maxParts {
		old := c.order[0]
		c.order = c.order[1:]
		c.cost -= c.entries[old].Size()
		delete(c.entries, old)
	}
	c.entries[key] = route
	c.order = append(c.order, key)
	c.cost += route.Size()
}

func (c *similarRouteCache) read(key routeVersion) (domain.RouteCoverage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	route, exists := c.entries[key]
	return route, exists
}

func (r *SegmentRepo) FindSimilarActivities(ctx context.Context, q domain.SimilarRouteQuery) ([]domain.SimilarActivity, error) {
	if q.SourceID == (domain.SourceID{}) {
		return nil, nil
	}
	db := dbtx(ctx, r.pool)
	rows, err := db.Query(ctx, `
		SELECT a.id, a.title, a.type, a.start_time, a.distance_m,
		       a.moving_duration_s, a.elapsed_duration_s, a.avg_heart_rate_bpm, a.avg_power_w,
		       src.id, src.heatmap_revision
		FROM activities a
		JOIN activity_sources src ON src.id = a.primary_stream_source_id AND src.activity_id = a.id
		  AND src.status <> 'detached'
		WHERE a.user_id = $1 AND a.id <> $2 AND a.type = $3
		  AND a.deleted_at IS NULL AND a.primary_stream_source_id IS NOT NULL`+communityReadSQL(ctx, "a.")+`
		  AND ($4::float8 <= 0 OR a.distance_m IS NULL OR a.distance_m BETWEEN $4 * .6 AND $4 / .6)
		ORDER BY a.start_time, a.id`, q.UserID.UUID(), q.ActivityID.UUID(), string(q.Type), q.DistanceM)
	if err != nil {
		return nil, fmt.Errorf("find route candidates: %w", err)
	}
	candidates, err := pgCollectRows(rows, func(row rowScanner) (similarCandidate, error) {
		c := similarCandidate{key: routeVersion{activityType: q.Type}}
		err := row.Scan(&c.ActivityID, &c.Title, &c.Type, &c.StartTime, &c.DistanceM, &c.MovingS, &c.ElapsedS,
			&c.AvgHeartRateBpm, &c.AvgPowerW, &c.key.source, &c.key.revision)
		return c, err
	}, "scan route candidate", "iterate route candidates")
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	target := routeVersion{source: q.SourceID, activityType: q.Type}
	err = db.QueryRow(ctx, `SELECT heatmap_revision FROM activity_sources
		WHERE id = $1 AND activity_id = $2 AND status <> 'detached'`,
		q.SourceID.UUID(), q.ActivityID.UUID()).Scan(&target.revision)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	routes, err := r.routeCoverages(ctx, []routeVersion{target})
	if err != nil {
		return nil, err
	}
	reference := routes[target]
	if reference.Size() == 0 {
		return nil, nil
	}
	var out []domain.SimilarActivity
	const batch = 24
	for start := 0; start < len(candidates); start += batch {
		items := candidates[start:min(start+batch, len(candidates))]
		keys := make([]routeVersion, 0, len(items))
		for _, candidate := range items {
			keys = append(keys, candidate.key)
		}
		routes, err := r.routeCoverages(ctx, keys)
		if err != nil {
			return nil, err
		}
		for _, candidate := range items {
			route := routes[candidate.key]
			if overlap := reference.Overlap(route); overlap >= domain.MinRouteOverlap {
				candidate.RouteOverlap = overlap
				out = append(out, candidate.SimilarActivity)
			}
		}
	}
	return out, nil
}

func (r *SegmentRepo) routeCoverages(ctx context.Context, keys []routeVersion) (map[routeVersion]domain.RouteCoverage, error) {
	out := make(map[routeVersion]domain.RouteCoverage, len(keys))
	missing := make(map[domain.SourceID]routeVersion)
	var ids []string
	var revisions []int64
	for _, key := range keys {
		if route, ok := r.routeCache.read(key); ok {
			out[key] = route
			continue
		}
		missing[key.source] = key
		ids = append(ids, key.source.String())
		revisions = append(revisions, key.revision)
	}
	if len(ids) == 0 {
		return out, nil
	}
	// Join the version in the stream query itself. A concurrent reimport
	// cannot associate a new stream with a cached older revision.
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		SELECT src.id, st.ts, COALESCE(st.recording_index, 0), st.latitude, st.longitude, st.distance_m, st.speed_mps
		FROM unnest($1::uuid[], $2::bigint[]) expected(source_id, revision)
		JOIN activity_sources src ON src.id = expected.source_id AND src.heatmap_revision = expected.revision
		LEFT JOIN activity_streams st ON st.activity_source_id = src.id
		ORDER BY src.id, st.ts`, ids, revisions)
	if err != nil {
		return nil, fmt.Errorf("read route samples: %w", err)
	}
	defer rows.Close()
	var source domain.SourceID
	var samples []domain.StreamSample
	finish := func() {
		if source == (domain.SourceID{}) {
			return
		}
		key := missing[source]
		route := domain.NewRouteCoverage(samples, key.activityType)
		r.routeCache.save(key, route)
		out[key] = route
		samples = samples[:0]
	}
	for rows.Next() {
		var id domain.SourceID
		var timestamp *time.Time
		var sample domain.StreamSample
		if err := rows.Scan(&id, &timestamp, &sample.RecordingIndex, &sample.Latitude, &sample.Longitude,
			&sample.DistanceM, &sample.SpeedMps); err != nil {
			return nil, fmt.Errorf("scan route sample: %w", err)
		}
		if id != source {
			finish()
			source = id
		}
		if timestamp != nil {
			sample.Timestamp = *timestamp
			samples = append(samples, sample)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	finish()
	return out, nil
}
