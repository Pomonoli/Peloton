package port

import "context"

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
