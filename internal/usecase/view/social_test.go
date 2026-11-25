package view

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/inmem"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type fakeLinks struct {
	port.ShareLinkRepo
	links map[string]domain.ShareLink
}

func (f *fakeLinks) GetActive(_ context.Context, token string) (domain.ShareLink, error) {
	l, ok := f.links[token]
	if !ok {
		return domain.ShareLink{}, domain.ErrNotFound
	}
	return l, nil
}

func TestProjectedAndShared(t *testing.T) {
	repo := inmem.NewActivityRepo()
	owner, viewer := domain.UserID(uuid.New()), domain.UserID(uuid.New())
	users := &fakeUsers{users: map[domain.UserID]domain.User{owner: {ID: owner, Username: "owner"}}}
	public := addActivity(repo, owner, domain.ActivityTypeRide, vNow, domain.PrivacyPublic)
	private := addActivity(repo, owner, domain.ActivityTypeRide, vNow, domain.PrivacyPrivate)
	shares := NewActivityShares(repo, users, NewVisibility())

	p, u, isOwner, err := shares.Projected(context.Background(), public, &viewer, false)
	if err != nil || isOwner || u.Username != "owner" || p.ID != public {
		t.Fatalf("projected %+v %v %v", p, isOwner, err)
	}
	if _, _, isOwner, err := shares.Projected(context.Background(), private, &owner, false); err != nil || !isOwner {
		t.Fatalf("owner on private: %v %v", isOwner, err)
	}
	if _, _, _, err := shares.Projected(context.Background(), private, &viewer, false); err != nil {
		t.Fatalf("member on legacy private ride: %v", err)
	}
	if _, _, _, err := shares.Projected(context.Background(), domain.ActivityID(uuid.New()), nil, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	links := &fakeLinks{links: map[string]domain.ShareLink{"tok": {Token: "tok", ActivityID: private}}}
	if p, u, err := shares.Shared(context.Background(), links, "tok", &viewer); err != nil || p.ID != private || u.Username != "owner" {
		t.Fatalf("shared %+v %v %v", p, u, err)
	}
	if _, _, err := shares.Shared(context.Background(), links, "nope", &viewer); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("bad token: %v", err)
	}
	if _, _, err := shares.Shared(context.Background(), links, "tok", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("anonymous shared read: %v", err)
	}
	if _, _, err := shares.Shared(context.Background(), nil, "tok", &viewer); err == nil {
		t.Fatal("no link repo must be unavailable")
	}
}

func TestProfilesGet(t *testing.T) {
	repo := inmem.NewActivityRepo()
	owner, viewer := domain.UserID(uuid.New()), domain.UserID(uuid.New())
	users := &fakeUsers{users: map[domain.UserID]domain.User{owner: {ID: owner, Username: "owner"}}}
	addActivity(repo, owner, domain.ActivityTypeRun, vNow, domain.PrivacyPublic)
	addActivity(repo, owner, domain.ActivityTypeRun, vNow.Add(-time.Hour), domain.PrivacyPrivate)
	p := NewProfiles(usersByName{users}, repo, NewVisibility())

	// Members see historical activities with either legacy privacy value.
	if v, err := p.Get(context.Background(), "Owner ", &owner); err != nil || !v.IsOwner || len(v.Activities) != 2 {
		t.Fatalf("owner %+v %v", v, err)
	}
	if v, err := p.Get(context.Background(), "owner", &viewer); err != nil || len(v.Activities) != 2 {
		t.Fatalf("member %+v %v", v, err)
	}
	if _, err := p.Get(context.Background(), "nobody", &viewer); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	// A legacy public profile still requires authentication.
	u := users.users[owner]
	u.ProfileIsPublic = true
	users.users[owner] = u
	if _, err := p.Get(context.Background(), "owner", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("anonymous: %v", err)
	}
}

type profileZoneReader struct {
	port.PrivacyZoneRepo
	zones   map[domain.UserID][]domain.PrivacyZone
	lookups []domain.UserID
	err     error
}

func (r *profileZoneReader) ListPrivacyZones(_ context.Context, owner domain.UserID) ([]domain.PrivacyZone, error) {
	r.lookups = append(r.lookups, owner)
	return r.zones[owner], r.err
}

func TestProfileHistoryAppliesOwnersZonesOnce(t *testing.T) {
	repo := inmem.NewActivityRepo()
	owner, viewer := domain.UserID(uuid.New()), domain.UserID(uuid.New())
	users := usersByName{&fakeUsers{users: map[domain.UserID]domain.User{owner: {ID: owner, Username: "rider"}}}}
	for i, lat := range []float64{50, 51} {
		id := addActivity(repo, owner, domain.ActivityTypeRide, vNow.Add(-time.Duration(i)*time.Hour), domain.PrivacyPrivate)
		act := repo.Activities[id]
		act.StartLat, act.StartLng, act.StartPlace = new(lat), new(4.0), "Ride start"
		repo.Activities[id] = act
	}
	zones := &profileZoneReader{zones: map[domain.UserID][]domain.PrivacyZone{
		owner:  {{Lat: 50, Lng: 4, RadiusM: 200}},
		viewer: {{Lat: 51, Lng: 4, RadiusM: 200}},
	}}
	profiles := NewProfiles(users, repo, &Visibility{Zones: zones})
	ctx := t.Context()
	page, err := profiles.GetPage(ctx, "rider", &viewer, 0)
	if err != nil || len(page.Activities) != 2 || len(zones.lookups) != 1 || zones.lookups[0] != owner {
		t.Fatalf("member page=%+v, lookups=%v, err=%v", page, zones.lookups, err)
	}
	hidden, visible := page.Activities[0], page.Activities[1]
	if !hidden.StartLocationRedacted || hidden.StartLat != nil || hidden.StartLng != nil || hidden.StartPlace != "" {
		t.Fatalf("owner's protected start was exposed: %+v", hidden)
	}
	if visible.StartLocationRedacted || visible.StartLat == nil || *visible.StartLat != 51 || visible.StartPlace != "Ride start" {
		t.Fatalf("start outside the owner's zone was masked: %+v", visible)
	}
	page, err = profiles.GetPage(ctx, "rider", &owner, 0)
	if err != nil || len(page.Activities) != 2 || page.Activities[0].StartLocationRedacted || len(zones.lookups) != 1 {
		t.Fatalf("owner page=%+v, lookups=%v, err=%v", page, zones.lookups, err)
	}
	page, err = profiles.GetPage(ctx, "rider", &viewer, 2)
	if err != nil || len(page.Activities) != 0 || len(zones.lookups) != 1 {
		t.Fatalf("empty page=%+v, lookups=%v, err=%v", page, zones.lookups, err)
	}
	zones.err = errors.New("privacy zone lookup unavailable")
	page, err = profiles.GetPage(ctx, "rider", &viewer, 0)
	if !errors.Is(err, zones.err) || len(page.Activities) != 0 {
		t.Fatalf("zone failure returned page=%+v, err=%v", page, err)
	}
}

// usersByName adds username lookup to fakeUsers.
type usersByName struct{ *fakeUsers }

func (u usersByName) GetUserByUsername(_ context.Context, name string) (domain.User, error) {
	for _, x := range u.users {
		if x.Username == name {
			return x, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}
