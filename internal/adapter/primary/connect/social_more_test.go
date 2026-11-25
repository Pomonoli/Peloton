package connect

import (
	"context"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect/v2"
	"github.com/google/uuid"

	v1 "github.com/opencairn-org/cairn-core/gen/proto/cairn/v1"
	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/inmem"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

type fakeLinksC struct {
	port.ShareLinkRepo
	link domain.ShareLink
}

func (f *fakeLinksC) GetActive(_ context.Context, token string) (domain.ShareLink, error) {
	if token != f.link.Token {
		return domain.ShareLink{}, domain.ErrNotFound
	}
	return f.link, nil
}

type usersByNameC struct{ *fakeUsers }

func (u usersByNameC) GetUserByUsername(_ context.Context, name string) (domain.User, error) {
	for _, x := range u.users {
		if x.Username == name {
			return x, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func TestSocialServerReads(t *testing.T) {
	repo := inmem.NewActivityRepo()
	me, owner := domain.UserID(uuid.New()), domain.UserID(uuid.New())
	users := &fakeUsers{users: map[domain.UserID]domain.User{me: {ID: me, Username: "me"}, owner: {ID: owner, Username: "owner", ProfileIsPublic: true}}}
	pub := seedPlain(repo, owner, domain.ActivityTypeRide, actNow, domain.PrivacyPublic)
	priv := seedPlain(repo, owner, domain.ActivityTypeRide, actNow.Add(-time.Hour), domain.PrivacyPrivate)
	shares := view.NewActivityShares(repo, users, view.NewVisibility())
	s := NewSocialServer(shares, &fakeContent{})
	s.Profiles = view.NewProfiles(usersByNameC{users}, repo, view.NewVisibility())
	s.ShareLinks = &fakeLinksC{link: domain.ShareLink{Token: "tok", ActivityID: priv}}
	s.Activities = repo
	ctx := context.WithValue(t.Context(), ctxKeyUserID, me)
	anon := t.Context()

	_, err := s.GetProfile(anon, &v1.GetProfileRequest{Username: "owner"})
	wantCode(t, err, connectrpc.CodeUnauthenticated)
	pr, err := s.GetProfile(ctx, &v1.GetProfileRequest{Username: "owner"})
	if err != nil || len(pr.GetActivities()) != 2 || pr.GetUser().GetUsername() != "owner" {
		t.Fatalf("profile %v %v", pr, err)
	}
	pa, err := s.GetProjectedActivity(ctx, &v1.GetProjectedActivityRequest{ActivityId: pub.String()})
	if err != nil || pa.GetIsOwner() || pa.GetOwner().GetUsername() != "owner" || pa.GetActivity().GetId() != pub.String() {
		t.Fatalf("projected %v %v", pa, err)
	}
	if legacy, err := s.GetProjectedActivity(ctx, &v1.GetProjectedActivityRequest{ActivityId: priv.String()}); err != nil || legacy.GetActivity().GetId() != priv.String() {
		t.Fatalf("projected historical activity %v %v", legacy, err)
	}
	_, err = s.GetSharedActivity(anon, &v1.GetSharedActivityRequest{Token: "tok"})
	wantCode(t, err, connectrpc.CodeUnauthenticated)
	sh, err := s.GetSharedActivity(ctx, &v1.GetSharedActivityRequest{Token: "tok"})
	if err != nil || sh.GetActivity().GetId() != priv.String() {
		t.Fatalf("shared %v %v", sh, err)
	}
	_, err = s.GetSharedActivity(ctx, &v1.GetSharedActivityRequest{Token: "nope"})
	wantCode(t, err, connectrpc.CodeNotFound)
	if rq, err := s.ListReviewQueue(ctx, &v1.ListReviewQueueRequest{}); err != nil || rq == nil {
		t.Fatalf("review queue %v %v", rq, err)
	}
	_, err = s.ListReviewQueue(anon, &v1.ListReviewQueueRequest{})
	wantCode(t, err, connectrpc.CodeUnauthenticated)
}
