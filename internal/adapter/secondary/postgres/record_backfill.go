package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// DeleteDetachedCyclingEfforts clears stale derived rows that can no longer
// be rebuilt from an attached recording.
func (r *BestEffortRepo) DeleteDetachedCyclingEfforts(ctx context.Context) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `
		DELETE FROM best_efforts b USING activity_sources src
		WHERE src.id = b.activity_source_id AND src.status = 'detached' AND b.activity_type = 'ride'`)
	if err != nil {
		return fmt.Errorf("clear detached cycling efforts: %w", err)
	}
	return nil
}

func (r *BestEffortRepo) ListCyclingSourcesForRecordBackfill(ctx context.Context, after domain.SourceID, limit int) ([]domain.SourceID, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		SELECT src.id
		FROM activity_sources src
		JOIN activities a ON a.id = src.activity_id
		WHERE src.id > $1 AND src.status <> 'detached' AND src.parsed @> '{"has_stream":true}'
			AND a.type = 'ride' AND a.deleted_at IS NULL
			AND 2 > (SELECT count(*) FROM best_efforts b
			         WHERE b.activity_source_id = src.id AND b.activity_type = 'ride'
			           AND b.metric = 'pace' AND b.window_kind = 'distance' AND b.window_value IN (20000, 50000))
		ORDER BY src.id LIMIT $2`, after.UUID(), limit)
	if err != nil {
		return nil, fmt.Errorf("list cycling sources for record backfill: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (domain.SourceID, error) {
		var id uuid.UUID
		err := row.Scan(&id)
		return domain.SourceID(id), err
	}, "scan backfill source", "iterate backfill sources")
}
