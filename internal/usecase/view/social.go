package view

import (
	"context"
	"fmt"
	"strings"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// ErrForbidden: the record exists but is not shared with this caller.
var ErrForbidden = port.NewActionError(port.ActionErrorForbidden, "not permitted")

// ActivityShares provides projections for the member-scoped share routes.
type ActivityShares struct {
	activities port.ActivityReader
	users      port.UserReader
	visibility *Visibility
}

func NewActivityShares(activities port.ActivityReader, users port.UserReader, visibility *Visibility) *ActivityShares {
	return &ActivityShares{activities: activities, users: users, visibility: visibility}
}

// Projected renders one activity for a member. Share links also exclude moderated
// and provider-restricted rides when used by the owner.
func (f *ActivityShares) Projected(ctx context.Context, id domain.ActivityID, viewer *domain.UserID, hasLink bool) (domain.ProjectedActivity, domain.User, bool, error) {
	a, err := f.activities.GetActivity(ctx, id)
	if err != nil {
		return domain.ProjectedActivity{}, domain.User{}, false, domain.ErrNotFound
	}
	if a.IsDeleted() || (hasLink && (a.HiddenByAdmin || a.ProviderRestricted)) {
		return domain.ProjectedActivity{}, domain.User{}, false, domain.ErrNotFound
	}
	vw, ok := f.visibility.Resolve(ctx, a, viewer)
	if !ok {
		return domain.ProjectedActivity{}, domain.User{}, false, ErrForbidden
	}
	owner, _ := f.users.GetUser(ctx, a.UserID)
	return domain.ProjectActivity(a, domain.ActivityCategories(), vw.Zones), owner, vw.IsOwner, nil
}

// Shared resolves an active share link for an authenticated member.
func (f *ActivityShares) Shared(ctx context.Context, links port.ShareLinkRepo, token string, viewer *domain.UserID) (domain.ProjectedActivity, domain.User, error) {
	if viewer == nil {
		return domain.ProjectedActivity{}, domain.User{}, ErrForbidden
	}
	if links == nil {
		return domain.ProjectedActivity{}, domain.User{}, ErrUnavailable
	}
	link, err := links.GetActive(ctx, token)
	if err != nil {
		return domain.ProjectedActivity{}, domain.User{}, domain.ErrNotFound
	}
	p, owner, _, err := f.Projected(ctx, link.ActivityID, viewer, true)
	return p, owner, err
}

// ProfileView is a member profile as one viewer sees it.
type ProfileView struct {
	User       domain.User
	IsOwner    bool
	Activities []domain.ProjectedActivity
	HasMore    bool
}

// Profiles are the signed-in profile reads (SocialService.GetProfile, GET /api/profiles/{username}).
type Profiles struct {
	users      port.UserReader
	activities port.ActivityReader
	visibility *Visibility
}

// NewProfiles wires member profile reads.
func NewProfiles(users port.UserReader, activities port.ActivityReader, visibility *Visibility) *Profiles {
	return &Profiles{users: users, activities: activities, visibility: visibility}
}

// profileRecent is how many recent activities a profile shows.
const profileRecent = 20

// Get resolves a member's profile and the first page of their history.
func (p *Profiles) Get(ctx context.Context, username string, viewer *domain.UserID) (ProfileView, error) {
	return p.GetPage(ctx, username, viewer, 0)
}

// GetPage pages a member's activity history for a signed-in viewer.
func (p *Profiles) GetPage(ctx context.Context, username string, viewer *domain.UserID, offset int) (ProfileView, error) {
	if viewer == nil || *viewer == (domain.UserID{}) {
		return ProfileView{}, ErrForbidden
	}
	u, err := p.users.GetUserByUsername(ctx, strings.ToLower(strings.TrimSpace(username)))
	if err != nil {
		return ProfileView{}, domain.ErrNotFound
	}
	v := ProfileView{User: u, IsOwner: *viewer == u.ID}
	activities, err := p.activities.ListMemberActivities(ctx, *viewer, u.ID, profileRecent+1, max(0, offset))
	if err != nil {
		return ProfileView{}, fmt.Errorf("list member activities: %w", err)
	}
	if len(activities) == 0 {
		return v, nil
	}
	zones, err := p.visibility.zonesForMember(ctx, u.ID, v.IsOwner)
	if err != nil {
		return ProfileView{}, fmt.Errorf("load member privacy zones: %w", err)
	}
	categories := domain.ActivityCategories()
	v.HasMore = len(activities) > profileRecent
	for _, act := range activities[:min(len(activities), profileRecent)] {
		if act.UserID == u.ID && memberCanView(act, viewer) {
			v.Activities = append(v.Activities, domain.ProjectActivity(act, categories, zones))
		}
	}
	return v, nil
}
