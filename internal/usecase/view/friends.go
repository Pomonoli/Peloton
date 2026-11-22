package view

import (
	"context"
	"fmt"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Friends provides the signed-in community directory and activity summaries.
type Friends struct {
	Directory port.CommunityDirectory
}

func (f *Friends) List(ctx context.Context, viewer domain.UserID) ([]domain.CommunityMember, error) {
	if viewer == (domain.UserID{}) {
		return nil, ErrForbidden
	}
	members, err := f.Directory.ListCommunityMembers(ctx, viewer)
	if err != nil {
		return nil, fmt.Errorf("list community members: %w", err)
	}
	return members, nil
}
