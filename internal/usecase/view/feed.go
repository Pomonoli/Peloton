package view

import (
	"context"
	"net/url"
	"path"
	"sort"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// FeedSource says where a feed row comes from.
type FeedSource string

const (
	FeedSelf      FeedSource = "self"
	FeedFollowing FeedSource = "following"
	FeedFederated FeedSource = "federated"
)

// FeedItem is one row of a feed. Exactly one of Own, Projected, Federated is set.
type FeedItem struct {
	Source FeedSource
	At     time.Time
	// Owner is set for Self and Following rows.
	Owner     domain.User
	Own       *domain.Activity
	Projected *domain.ProjectedActivity
	Federated *domain.FederatedFeedItem
}

// Feeds assembles the social feeds (GET /api/feed/home, SocialService.ListHomeFeed).
type Feeds struct {
	activities        port.ActivityReader
	users             port.UserReader
	visibility        *Visibility
	federated         port.FederationFeedRepo // optional
	federationEnabled bool
}

// NewFeeds wires the feeds. federated may be nil; federationEnabled gates
// federated rows on the instance flag, not just the wired repo, so items
// received while federation was on stay dormant when it is switched off.
func NewFeeds(activities port.ActivityReader, users port.UserReader, visibility *Visibility, federated port.FederationFeedRepo, federationEnabled bool) *Feeds {
	return &Feeds{activities: activities, users: users, visibility: visibility, federated: federated, federationEnabled: federationEnabled}
}

// Paging bounds of the home feed.
const (
	FeedDefaultLimit = 30
	FeedMaxLimit     = 100
)

// Home merges local community activities and any enabled federated feed.
func (f *Feeds) Home(ctx context.Context, me domain.UserID, limit, offset int) []FeedItem {
	limit, offset = feedPage(limit, offset)
	if !f.federationEnabled || f.federated == nil {
		return f.Following(ctx, me, limit, offset)
	}
	// Read enough rows from each source to page their combined timeline.
	count := offset + limit
	acts, _ := f.activities.ListCommunityActivities(ctx, me, nil, count, 0)
	items := append(f.Project(ctx, acts, &me), f.federatedRows(ctx, me, count, 0)...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.After(items[j].At) })
	return items[min(offset, len(items)):min(count, len(items))]
}

// Following is the local community feed, exposed at the existing feed endpoint.
func (f *Feeds) Following(ctx context.Context, me domain.UserID, limit, offset int) []FeedItem {
	limit, offset = feedPage(limit, offset)
	acts, _ := f.activities.ListCommunityActivities(ctx, me, nil, limit, offset)
	return f.Project(ctx, acts, &me)
}

func (f *Feeds) federatedRows(ctx context.Context, me domain.UserID, limit, offset int) []FeedItem {
	rows, _ := f.federated.ListForUser(ctx, me, limit, offset)
	items := make([]FeedItem, 0, len(rows))
	for i := range rows {
		it := rows[i]
		items = append(items, FeedItem{Source: FeedFederated, At: it.Published, Federated: &it})
	}
	return items
}

func feedPage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = FeedDefaultLimit
	}
	if limit > FeedMaxLimit {
		limit = FeedMaxLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// RemoteHandle renders an actor URL as @user@host for display.
func RemoteHandle(actorID string) string {
	if u, err := url.Parse(actorID); err == nil && u.Host != "" {
		return "@" + path.Base(u.Path) + "@" + u.Host
	}
	return actorID
}
