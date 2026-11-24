package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// VisibilityRepo stores users’ geographic privacy zones.
type VisibilityRepo struct {
	pool *pgxpool.Pool
}

func NewVisibilityRepo(pool *pgxpool.Pool) *VisibilityRepo {
	return &VisibilityRepo{pool: pool}
}

func (r *VisibilityRepo) ListPrivacyZones(ctx context.Context, userID domain.UserID) ([]domain.PrivacyZone, error) {
	db := dbtx(ctx, r.pool)
	rows, err := db.Query(ctx,
		`SELECT id, user_id, label, lat, lng, radius_m, created_at
		   FROM privacy_zones WHERE user_id = $1 ORDER BY created_at ASC`, userID.UUID())
	if err != nil {
		return nil, fmt.Errorf("list privacy zones: %w", err)
	}
	defer rows.Close()
	var out []domain.PrivacyZone
	for rows.Next() {
		var z domain.PrivacyZone
		var id, uid uuid.UUID
		if err := rows.Scan(&id, &uid, &z.Label, &z.Lat, &z.Lng, &z.RadiusM, &z.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan zone: %w", err)
		}
		z.ID = domain.PrivacyZoneID(id)
		z.UserID = domain.UserID(uid)
		out = append(out, z)
	}
	return out, rows.Err()
}

func (r *VisibilityRepo) AddPrivacyZone(ctx context.Context, z domain.PrivacyZone) (domain.PrivacyZoneID, error) {
	db := dbtx(ctx, r.pool)
	id, err := uuid.NewV7()
	if err != nil {
		return domain.PrivacyZoneID{}, err
	}
	radius := z.RadiusM
	if radius <= 0 {
		radius = 200
	}
	_, err = db.Exec(ctx,
		`INSERT INTO privacy_zones (id, user_id, label, lat, lng, radius_m) VALUES ($1,$2,$3,$4,$5,$6)`,
		id, z.UserID.UUID(), z.Label, z.Lat, z.Lng, radius,
	)
	if err != nil {
		return domain.PrivacyZoneID{}, fmt.Errorf("add privacy zone: %w", err)
	}
	return domain.PrivacyZoneID(id), nil
}

func (r *VisibilityRepo) DeletePrivacyZone(ctx context.Context, id domain.PrivacyZoneID, userID domain.UserID) error {
	db := dbtx(ctx, r.pool)
	tag, err := db.Exec(ctx, `DELETE FROM privacy_zones WHERE id = $1 AND user_id = $2`, id.UUID(), userID.UUID())
	if err != nil {
		return fmt.Errorf("delete privacy zone: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
