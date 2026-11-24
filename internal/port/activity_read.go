package port

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

type communityViewerKey struct{}

func WithCommunityViewer(ctx context.Context, viewer domain.UserID) context.Context {
	return context.WithValue(ctx, communityViewerKey{}, viewer)
}

func CommunityViewer(ctx context.Context) (domain.UserID, bool) {
	viewer, ok := ctx.Value(communityViewerKey{}).(domain.UserID)
	return viewer, ok
}

type communityActivityReadKey struct{}

// WithActivityReadViewer retains whether a read is for someone other than the owner.
func WithActivityReadViewer(ctx context.Context, isOwner bool) context.Context {
	if isOwner {
		return ctx
	}
	return context.WithValue(ctx, communityActivityReadKey{}, true)
}

// IsCommunityActivityRead reports whether a read must omit administratively hidden activities.
func IsCommunityActivityRead(ctx context.Context) bool {
	memberRead, _ := ctx.Value(communityActivityReadKey{}).(bool)
	return memberRead
}
