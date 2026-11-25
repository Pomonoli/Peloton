package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// ShareLinkRepo implements port.ShareLinkRepo over activity_share_links.
type ShareLinkRepo struct{ pool *pgxpool.Pool }

func NewShareLinkRepo(pool *pgxpool.Pool) *ShareLinkRepo { return &ShareLinkRepo{pool: pool} }

func (r *ShareLinkRepo) Create(ctx context.Context, l domain.ShareLink) error {
	db := dbtx(ctx, r.pool)
	_, err := db.Exec(ctx,
		`INSERT INTO activity_share_links (token, activity_id, created_by) VALUES ($1,$2,$3)`,
		l.Token, l.ActivityID.UUID(), l.CreatedBy.UUID())
	if err != nil {
		return fmt.Errorf("create share link: %w", err)
	}
	return nil
}

func (r *ShareLinkRepo) GetActive(ctx context.Context, token string) (domain.ShareLink, error) {
	db := dbtx(ctx, r.pool)
	var (
		l       domain.ShareLink
		actID   uuid.UUID
		creator uuid.UUID
	)
	err := db.QueryRow(ctx,
		`SELECT token, activity_id, created_by, created_at, revoked_at
		   FROM activity_share_links WHERE token = $1 AND revoked_at IS NULL`,
		token).Scan(&l.Token, &actID, &creator, &l.CreatedAt, &l.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ShareLink{}, domain.ErrNotFound
		}
		return domain.ShareLink{}, fmt.Errorf("get share link: %w", err)
	}
	l.ActivityID = domain.ActivityID(actID)
	l.CreatedBy = domain.UserID(creator)
	return l, nil
}

func (r *ShareLinkRepo) ListForActivity(ctx context.Context, activityID domain.ActivityID) ([]domain.ShareLink, error) {
	db := dbtx(ctx, r.pool)
	rows, err := db.Query(ctx,
		`SELECT token, activity_id, created_by, created_at, revoked_at
		   FROM activity_share_links WHERE activity_id = $1 ORDER BY created_at DESC`,
		activityID.UUID())
	if err != nil {
		return nil, fmt.Errorf("list share links: %w", err)
	}
	defer rows.Close()
	var out []domain.ShareLink
	for rows.Next() {
		var l domain.ShareLink
		var actID, creator uuid.UUID
		if err := rows.Scan(&l.Token, &actID, &creator, &l.CreatedAt, &l.RevokedAt); err != nil {
			return nil, err
		}
		l.ActivityID = domain.ActivityID(actID)
		l.CreatedBy = domain.UserID(creator)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *ShareLinkRepo) Revoke(ctx context.Context, token string, owner domain.UserID) error {
	db := dbtx(ctx, r.pool)
	_, err := db.Exec(ctx,
		`UPDATE activity_share_links SET revoked_at = now()
		   WHERE token = $1 AND created_by = $2 AND revoked_at IS NULL`,
		token, owner.UUID())
	if err != nil {
		return fmt.Errorf("revoke share link: %w", err)
	}
	return nil
}
