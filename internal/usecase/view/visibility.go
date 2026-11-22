// Package view holds the read-side use cases shared by the REST and Connect adapters.
package view

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Visibility resolves activity reads inside the authenticated community.
type Visibility struct{}

func NewVisibility() *Visibility { return &Visibility{} }

// For returns the activity categories visible to a local member. The link
// argument is retained for callers that also accept existing share tokens.
func (v *Visibility) For(_ context.Context, act *domain.Activity, viewer *domain.UserID, _ bool) domain.CategorySet {
	return domain.CommunityActivityCategories(act, viewer)
}

// Viewer describes how a member may look at an activity.
type Viewer struct {
	Cats    domain.CategorySet
	Zones   []domain.PrivacyZone
	IsOwner bool
}

func (v Viewer) ActivityReadContext(ctx context.Context) context.Context {
	return port.WithActivityReadViewer(ctx, v.IsOwner)
}

func (v *Visibility) Resolve(ctx context.Context, act domain.Activity, viewer *domain.UserID, hasLink bool) (Viewer, bool) {
	cats := v.For(ctx, &act, viewer, hasLink)
	if len(cats) == 0 {
		return Viewer{}, false
	}
	return Viewer{Cats: cats, IsOwner: *viewer == act.UserID}, true
}
