package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/territory"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Bump when the distance calculation changes.
const territoryAlgorithm = 1

const territoryRidesSQL = `
	SELECT a.id, a.user_id, a.start_time, p.id AS source, p.heatmap_revision AS revision,
		c.activity_id IS NOT NULL AS ready
	FROM activities a ` + heatmapPrimarySQL + `
	JOIN users u ON u.id=a.user_id AND u.status <> 'deleted'
	LEFT JOIN territory_activity_cache c ON c.activity_id=a.id AND c.source_id=p.id
		AND c.source_revision=p.heatmap_revision AND c.algorithm=$3
	WHERE a.type='ride' AND a.deleted_at IS NULL AND ` + sharedActivitySQL + `
		AND a.primary_stream_source_id IS NOT NULL AND a.start_time >= $1 AND a.start_time <= $2`

// The assigned color index is retained through renames and account removals.
const territoryRidersSQL = `
	SELECT id, username, display_name, territory_color_index - 1 AS color_index
	FROM users WHERE status <> 'deleted'`

type TerritoryRepo struct{ pool *pgxpool.Pool }

func NewTerritoryRepo(pool *pgxpool.Pool) *TerritoryRepo {
	return &TerritoryRepo{pool: pool}
}

func (r *TerritoryRepo) ReadTerritory(ctx context.Context, now time.Time, knownVersion string) (port.TerritoryRead, error) {
	var out port.TerritoryRead
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	args := []any{now.Add(-port.TerritoryWindow), now, territoryAlgorithm}
	err = tx.QueryRow(ctx, `
		WITH rides AS (`+territoryRidesSQL+`), riders AS (`+territoryRidersSQL+`)
		SELECT md5(jsonb_build_array($3::smallint,
			(SELECT jsonb_agg(jsonb_build_array(id, user_id, source, revision) ORDER BY id) FROM rides),
			(SELECT jsonb_agg(to_jsonb(riders) ORDER BY id) FROM riders),
			(SELECT jsonb_agg(jsonb_build_array(z.user_id, z.lat, z.lng, z.radius_m) ORDER BY z.id)
			 FROM privacy_zones z JOIN riders u ON u.id=z.user_id)
		)::text), (SELECT count(*) FROM rides WHERE NOT ready)`, args...).Scan(&out.Version, &out.Pending)
	if err != nil {
		return out, fmt.Errorf("read territory version: %w", err)
	}

	switch {
	case out.Pending > 0:
		rows, err := tx.Query(ctx, `SELECT id, source, revision FROM (`+territoryRidesSQL+`) rides
			WHERE NOT ready ORDER BY start_time DESC, id LIMIT 24`, args...)
		if err != nil {
			return out, fmt.Errorf("read uncached territory rides: %w", err)
		}
		out.Rides, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (port.TerritoryRide, error) {
			var ride port.TerritoryRide
			err := row.Scan(&ride.Activity, &ride.Source, &ride.Revision)
			return ride, err
		})
		if err != nil {
			return out, err
		}
	case out.Version != knownVersion:
		out.Riders, err = readTerritoryRiders(ctx, tx)
		if err != nil {
			return out, err
		}
		out.Scores, err = readTerritoryScores(ctx, tx, now)
		if err != nil {
			return out, err
		}
	}
	return out, tx.Commit(ctx)
}

func readTerritoryScores(ctx context.Context, tx pgx.Tx, now time.Time) ([]port.TerritoryScore, error) {
	rows, err := tx.Query(ctx, `
		WITH rides AS (`+territoryRidesSQL+`)
		SELECT c.cell, r.user_id, sum(c.distance_mm)::bigint AS distance_mm
		FROM rides r JOIN territory_activity_cells c ON c.activity_id=r.id
		WHERE r.ready GROUP BY c.cell, r.user_id ORDER BY c.cell, distance_mm DESC, r.user_id`, now.Add(-port.TerritoryWindow), now, territoryAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("aggregate territory distances: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (port.TerritoryScore, error) {
		var score port.TerritoryScore
		var cell int64
		err := row.Scan(&cell, &score.Rider, &score.DistanceMM)
		if err != nil {
			return score, err
		}
		if cell <= 0 {
			return score, fmt.Errorf("invalid stored territory cell: %d", cell)
		}
		score.Cell = territory.Cell(cell)
		return score, nil
	})
}

func readTerritoryRiders(ctx context.Context, tx pgx.Tx) ([]port.TerritoryRider, error) {
	rows, err := tx.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.color_index, z.lat, z.lng, z.radius_m
		FROM (`+territoryRidersSQL+`) u LEFT JOIN privacy_zones z ON z.user_id=u.id
		ORDER BY u.color_index, z.id`)
	if err != nil {
		return nil, fmt.Errorf("read territory riders: %w", err)
	}
	defer rows.Close()
	var riders []port.TerritoryRider
	for rows.Next() {
		var rider port.TerritoryRider
		var lat, lng, radius *float64
		if err := rows.Scan(&rider.UserID, &rider.Username, &rider.DisplayName, &rider.ColorIndex, &lat, &lng, &radius); err != nil {
			return nil, err
		}
		if len(riders) == 0 || riders[len(riders)-1].UserID != rider.UserID {
			riders = append(riders, rider)
		}
		if lat != nil {
			current := &riders[len(riders)-1]
			current.Zones = append(current.Zones, domain.PrivacyZone{Lat: *lat, Lng: *lng, RadiusM: *radius})
		}
	}
	return riders, rows.Err()
}

func (r *TerritoryRepo) SaveTerritory(ctx context.Context, ride port.TerritoryRide, distances map[territory.Cell]int64) error {
	return NewTxManager(r.pool).InTx(ctx, func(ctx context.Context) error {
		tx, _ := txFromCtx(ctx)
		// Lock the activity as the performance cache does. A concurrent source
		// replacement can only publish rows still matching the live revision.
		var id domain.ActivityID
		err := tx.QueryRow(ctx, `SELECT a.id FROM activities a `+heatmapPrimarySQL+`
			WHERE a.id=$1 AND p.id=$2 AND p.heatmap_revision=$3 AND a.deleted_at IS NULL
			FOR UPDATE OF a`, ride.Activity.UUID(), ride.Source.UUID(), ride.Revision).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO territory_activity_cache (activity_id, source_id, source_revision, algorithm)
			VALUES ($1,$2,$3,$4) ON CONFLICT (activity_id) DO UPDATE
			SET source_id=EXCLUDED.source_id, source_revision=EXCLUDED.source_revision, algorithm=EXCLUDED.algorithm
			WHERE (territory_activity_cache.source_id, territory_activity_cache.source_revision, territory_activity_cache.algorithm)
				IS DISTINCT FROM (EXCLUDED.source_id, EXCLUDED.source_revision, EXCLUDED.algorithm)`,
			id.UUID(), ride.Source.UUID(), ride.Revision, territoryAlgorithm)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return replaceTerritoryCells(ctx, tx, id, distances)
	})
}

func replaceTerritoryCells(ctx context.Context, tx pgx.Tx, id domain.ActivityID, distances map[territory.Cell]int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM territory_activity_cells WHERE activity_id=$1`, id.UUID()); err != nil {
		return err
	}
	if len(distances) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(distances))
	for cell, mm := range distances {
		if cell > math.MaxInt64 {
			return fmt.Errorf("invalid territory cell: %s", cell)
		}
		rows = append(rows, []any{id.UUID(), int64(cell), mm})
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"territory_activity_cells"}, []string{"activity_id", "cell", "distance_mm"}, pgx.CopyFromRows(rows))
	return err
}
