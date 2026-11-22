package port

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

type CommunityDirectory interface {
	ListCommunityMembers(ctx context.Context, viewer domain.UserID) ([]domain.CommunityMember, error)
}
