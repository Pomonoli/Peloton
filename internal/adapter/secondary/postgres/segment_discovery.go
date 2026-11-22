package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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

// ListPendingGarminSegmentScans finds live Garmin recordings whose route
// discovery has not completed. Keyset paging lets a later ride proceed when
// the provider temporarily fails for an earlier one.
func (r *SegmentRepo) ListPendingGarminSegmentScans(ctx context.Context, after domain.SourceID, limit int) ([]port.GarminSegmentScan, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		SELECT src.id, src.user_id
		FROM activity_sources src JOIN activities a ON a.id = src.activity_id
		WHERE src.provider = 'garmin' AND src.status = 'active'
		  AND src.parsed @> '{"has_stream":true}' AND src.garmin_segments_scanned_at IS NULL
		  AND a.deleted_at IS NULL AND NOT a.hidden_by_admin AND NOT a.provider_restricted AND a.type = 'ride' AND src.provider <> 'strava'
		  AND src.id > $1
		ORDER BY src.id LIMIT $2`, after.UUID(), limit)
	if err != nil {
		return nil, fmt.Errorf("list pending Garmin segment scans: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (port.GarminSegmentScan, error) {
		var source, user uuid.UUID
		err := row.Scan(&source, &user)
		return port.GarminSegmentScan{SourceID: domain.SourceID(source), UserID: domain.UserID(user)}, err
	}, "scan pending Garmin activity", "iterate pending Garmin activities")
}

func (r *SegmentRepo) CompleteGarminSegmentScan(ctx context.Context, source domain.SourceID, updatedAt time.Time) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `
		UPDATE activity_sources SET garmin_segments_scanned_at = now()
		WHERE id = $1 AND provider = 'garmin' AND updated_at = $2`, source.UUID(), updatedAt)
	if err != nil {
		return fmt.Errorf("mark Garmin segment scan complete: %w", err)
	}
	return nil
}
