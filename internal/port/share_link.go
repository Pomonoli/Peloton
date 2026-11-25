package port

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// ShareLinkRepo persists activity share links (migration 34).
type ShareLinkRepo interface {
	Create(ctx context.Context, link domain.ShareLink) error
	// GetActive resolves a non-revoked token. ErrNotFound on miss/revoked.
	GetActive(ctx context.Context, token string) (domain.ShareLink, error)
	ListForActivity(ctx context.Context, activityID domain.ActivityID) ([]domain.ShareLink, error)
	Revoke(ctx context.Context, token string, owner domain.UserID) error
}
