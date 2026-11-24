package postgres

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/heatmap"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type PerformanceHeatmapRepo struct{ pool *pgxpool.Pool }

func NewPerformanceHeatmapRepo(pool *pgxpool.Pool) *PerformanceHeatmapRepo {
	return &PerformanceHeatmapRepo{pool: pool}
}

// Bump the prefix when the bin format or aggregation changes.
const heatmapSignatureSQL = `md5('v2:' || p.id::text || ':' ||
	string_agg(src.id::text || ':' || src.heatmap_revision::text || ':' ||
		COALESCE(src.parsed->>'power_recorded', ''), ',' ORDER BY src.id))`

const heatmapSourcesSQL = `
	FROM activities a
	JOIN activity_sources p ON p.id = a.primary_stream_source_id
		AND p.activity_id = a.id AND p.status <> 'detached' AND p.provider <> 'derived'
	JOIN activity_sources src ON src.activity_id = a.id
		AND src.status <> 'detached' AND src.provider <> 'derived'
		AND (src.id = p.id OR src.parsed @> '{"has_stream":true}')`

func (r *PerformanceHeatmapRepo) HeatmapActivities(ctx context.Context, member domain.UserID, filter port.ActivityListFilter) ([]port.HeatmapActivity, error) {
	conditions, args := activityFilterConds(member, filter, "")
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		WITH filtered AS (
			SELECT id FROM activities WHERE `+strings.Join(conditions, " AND ")+communityReadSQL(ctx, "")+communityMapSQL(ctx, "")+`
		), candidates AS (
			SELECT a.id, p.id AS primary_id, array_agg(src.id ORDER BY src.id) AS sources,
				`+heatmapSignatureSQL+` AS signature
			`+heatmapSourcesSQL+`
			JOIN filtered f ON f.id = a.id
			GROUP BY a.id,p.id
		)
		SELECT f.id, c.primary_id, c.sources, COALESCE(c.signature, ''), h.activity_id IS NOT NULL, COALESCE(h.has_track, false)
		FROM filtered f
		LEFT JOIN candidates c ON c.id=f.id
		LEFT JOIN performance_heatmap_cache h ON h.activity_id=f.id AND h.signature=c.signature
		ORDER BY f.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list heatmap activities: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (port.HeatmapActivity, error) {
		var out port.HeatmapActivity
		var id uuid.UUID
		var primary *uuid.UUID
		var sources []uuid.UUID
		err := row.Scan(&id, &primary, &sources, &out.Version, &out.Ready, &out.WithTrack)
		out.ID = domain.ActivityID(id)
		if primary != nil {
			out.Primary = domain.SourceID(*primary)
		}
		for _, sid := range sources {
			out.Sources = append(out.Sources, domain.SourceID(sid))
		}
		return out, err
	}, "scan heatmap activity", "iterate heatmap activities")
}

func (r *PerformanceHeatmapRepo) HeatmapSamples(ctx context.Context, source domain.SourceID, gps bool) ([]domain.StreamSample, error) {
	fields := "NULL::float8, NULL::float8, NULL::float8, NULL::float8, NULL::float8, NULL::real"
	if gps {
		fields = "st.latitude, st.longitude, st.altitude_m, st.distance_m, st.speed_mps, st.grade"
	}
	rows, err := dbtx(ctx, r.pool).Query(ctx, `SELECT st.ts, COALESCE(st.recording_index, 0), `+fields+`, st.heart_rate_bpm,
		CASE WHEN `+measuredPowerSQL+` THEN st.power_w END
		FROM activity_sources src JOIN activity_streams st ON st.activity_source_id=src.id
		WHERE src.id=$1 AND src.status <> 'detached' ORDER BY st.ts`, source.UUID())
	if err != nil {
		return nil, fmt.Errorf("read heatmap samples: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (domain.StreamSample, error) {
		var sample domain.StreamSample
		err := row.Scan(&sample.Timestamp, &sample.RecordingIndex, &sample.Latitude, &sample.Longitude, &sample.AltitudeM, &sample.DistanceM, &sample.SpeedMps, &sample.Grade,
			&sample.HeartRateBpm, &sample.PowerW)
		return sample, err
	}, "scan heatmap sample", "iterate heatmap samples")
}

type heatmapTileKey struct {
	level heatmap.Level
	tile  heatmap.Tile
}

func (r *PerformanceHeatmapRepo) SaveHeatmapBins(ctx context.Context, activity port.HeatmapActivity, bins [3][]heatmap.Bin) error {
	tiles := make(map[heatmapTileKey][]heatmap.Bin)
	for level, group := range bins {
		for _, bin := range group {
			key := heatmapTileKey{heatmap.Level(level), bin.Key.Tile(heatmap.Level(level))}
			tiles[key] = append(tiles[key], bin)
		}
	}
	return NewTxManager(r.pool).InTx(ctx, func(ctx context.Context) error {
		tx, _ := txFromCtx(ctx)
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM activities WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, activity.ID.UUID()).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var current string
		err = tx.QueryRow(ctx, `SELECT `+heatmapSignatureSQL+heatmapSourcesSQL+` WHERE a.id=$1 GROUP BY p.id`, id).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && current != activity.Version) {
			// The use case re-reads readiness after the batch, including any newer version.
			return nil
		}
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO performance_heatmap_cache(activity_id,signature,has_track) VALUES ($1,$2,$3)
			ON CONFLICT (activity_id) DO UPDATE SET signature=EXCLUDED.signature,has_track=EXCLUDED.has_track
			WHERE performance_heatmap_cache.signature <> EXCLUDED.signature`, id, current, len(tiles) > 0)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			// A concurrent request can already have cached this version. Verify
			// that before treating a skipped write as a success.
			var ready bool
			err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM performance_heatmap_cache
				WHERE activity_id=$1 AND signature=$2)`, id, current).Scan(&ready)
			if err != nil {
				return err
			}
			if !ready {
				return fmt.Errorf("heatmap cache write skipped for activity %s", id)
			}
			return nil
		}
		if _, err = tx.Exec(ctx, `DELETE FROM performance_heatmap_tiles WHERE activity_id=$1`, id); err != nil {
			return err
		}
		rows := make([][]any, 0, len(tiles))
		for key, group := range tiles {
			payload, err := binary.Append(make([]byte, 0, len(group)*heatmapBinSize), binary.LittleEndian, group)
			if err != nil {
				return err
			}
			rows = append(rows, []any{id, int16(key.level), int16(key.tile.X), int16(key.tile.Y), payload})
		}
		written, err := tx.CopyFrom(ctx, pgx.Identifier{"performance_heatmap_tiles"}, []string{"activity_id", "level", "tile_x", "tile_y", "bins"}, pgx.CopyFromRows(rows))
		if err != nil {
			return err
		}
		if written != int64(len(rows)) {
			return fmt.Errorf("heatmap cache wrote %d of %d tiles for activity %s", written, len(rows), id)
		}
		return nil
	})
}

var heatmapBinSize = binary.Size(heatmap.Bin{})

func (r *PerformanceHeatmapRepo) VisitHeatmapBins(ctx context.Context, member domain.UserID, activities []port.HeatmapActivity, viewport port.HeatmapViewport, visit func(heatmap.Bin)) error {
	ids := make([]uuid.UUID, 0, len(activities))
	versions := make([]string, 0, len(activities))
	for _, activity := range activities {
		if activity.WithTrack {
			ids = append(ids, activity.ID.UUID())
			versions = append(versions, activity.Version)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	args := []any{ids, versions, member.UUID(), int16(viewport.Level)}
	filter := ""
	if b := viewport.Bounds; b != nil {
		low, high := heatmap.TileAt(heatmap.Project(b.MinLon, b.MinLat)), heatmap.TileAt(heatmap.Project(b.MaxLon, b.MaxLat))
		// A cell may straddle a tile boundary. Read the adjacent tiles too,
		// so lines and their full cumulative sums don't flicker at seams.
		args = append(args, int16(max(0, low.Y-1)), int16(min(heatmap.TileCount-1, high.Y+1)))
		filter = " AND t.tile_y BETWEEN $5 AND $6"
		width := high.X - low.X + 1
		if b.MinLon > b.MaxLon {
			width += heatmap.TileCount
		}
		if width < heatmap.TileCount-2 {
			west, east := (low.X+heatmap.TileCount-1)%heatmap.TileCount, (high.X+1)%heatmap.TileCount
			args = append(args, int16(west), int16(east))
			filter += ` AND (($7::smallint <= $8::smallint AND t.tile_x BETWEEN $7 AND $8)
				OR ($7 > $8 AND (t.tile_x >= $7 OR t.tile_x <= $8)))`
		}
	}
	showHR, showPower := "true", "true"
	if port.IsCommunityActivityRead(ctx) {
		viewer := communityViewerSQL(ctx)
		showHR = sharedCategorySQL("a.", viewer, "'hr'")
		showPower = sharedCategorySQL("a.", viewer, "'power'")
	}
	rows, err := dbtx(ctx, r.pool).Query(ctx, `SELECT t.bins, `+showHR+`, `+showPower+`
		FROM unnest($1::uuid[], $2::text[]) f(id,signature)
		JOIN activities a ON a.id=f.id AND a.user_id=$3 AND a.deleted_at IS NULL AND a.type='ride'
		JOIN performance_heatmap_cache h ON h.activity_id=f.id AND h.signature=f.signature
		JOIN performance_heatmap_tiles t ON t.activity_id=f.id AND t.level=$4
		WHERE TRUE`+communityReadSQL(ctx, "a.")+communityMapSQL(ctx, "a.")+filter+` ORDER BY t.activity_id, t.tile_x, t.tile_y`, args...)
	if err != nil {
		return fmt.Errorf("read heatmap tiles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var hr, power bool
		if err := rows.Scan(&raw, &hr, &power); err != nil {
			return err
		}
		if len(raw)%heatmapBinSize != 0 {
			return fmt.Errorf("invalid heatmap tile length")
		}
		bins := make([]heatmap.Bin, len(raw)/heatmapBinSize)
		if _, err := binary.Decode(raw, binary.LittleEndian, bins); err != nil {
			return err
		}
		for _, bin := range bins {
			if !hr {
				bin.HeartRate = heatmap.Weighted{}
			}
			if !power {
				bin.Power = heatmap.Weighted{}
			}
			visit(bin)
		}
	}
	return rows.Err()
}
