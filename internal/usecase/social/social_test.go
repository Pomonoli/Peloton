package social

import (
	"context"
	"github.com/google/uuid"
	"testing"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type fakeVisibility struct {
	port.PrivacyZoneRepo
	zone domain.PrivacyZone
}

func (f *fakeVisibility) AddPrivacyZone(_ context.Context, z domain.PrivacyZone) (domain.PrivacyZoneID, error) {
	f.zone = z
	return domain.PrivacyZoneID(uuid.New()), nil
}

func TestPrivacyZoneDefaults(t *testing.T) {
	ctx := t.Context()
	me := domain.UserID{1}
	vis := &fakeVisibility{}
	p := &Privacy{Visibility: vis}

	if _, err := p.AddZone(ctx, me, domain.PrivacyZone{UserID: domain.UserID{2}, Lat: 1, Lng: 2}); err != nil {
		t.Fatal(err)
	}
	if vis.zone.UserID != me || vis.zone.RadiusM != defaultZoneRadiusM {
		t.Fatalf("zone = %+v", vis.zone)
	}
}
