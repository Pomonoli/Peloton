package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func (r *SegmentRepo) DiscoveryWindow(ctx context.Context, provider string, window port.SegmentDiscoveryWindow, since time.Time) (bool, bool, error) {
	var saturated bool
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT saturated FROM segment_discovery_windows
		WHERE provider = $1 AND depth = $2 AND tile_x = $3 AND tile_y = $4 AND fetched_at >= $5`,
		provider, window.Depth, window.X, window.Y, since).Scan(&saturated)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	return err == nil, saturated, err
}

func (r *SegmentRepo) SaveDiscoveryWindow(ctx context.Context, provider string, window port.SegmentDiscoveryWindow, fetchedAt time.Time, saturated bool) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `
		INSERT INTO segment_discovery_windows (provider, depth, tile_x, tile_y, fetched_at, saturated)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (provider, depth, tile_x, tile_y) DO UPDATE SET
			fetched_at = EXCLUDED.fetched_at, saturated = EXCLUDED.saturated
		WHERE segment_discovery_windows.fetched_at <= EXCLUDED.fetched_at`,
		provider, window.Depth, window.X, window.Y, fetchedAt, saturated)
	return err
}

func (r *SegmentRepo) LockSegmentMatch(ctx context.Context, id domain.SourceID) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 812783))", id.String())
	return err
}
