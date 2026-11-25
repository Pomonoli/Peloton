// Package social manages member privacy zones.
package social

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Privacy manages the caller's geographic privacy zones.
type Privacy struct {
	Visibility port.PrivacyZoneRepo
}

// defaultZoneRadiusM applies when a zone is added without a radius.
const defaultZoneRadiusM = 200

// Zones lists the caller's privacy zones.
func (p *Privacy) Zones(ctx context.Context, me domain.UserID) ([]domain.PrivacyZone, error) {
	return p.Visibility.ListPrivacyZones(ctx, me)
}

// AddZone stores a zone owned by the caller.
func (p *Privacy) AddZone(ctx context.Context, me domain.UserID, z domain.PrivacyZone) (domain.PrivacyZoneID, error) {
	z.UserID = me
	if z.RadiusM <= 0 {
		z.RadiusM = defaultZoneRadiusM
	}
	return p.Visibility.AddPrivacyZone(ctx, z)
}

// DeleteZone removes one of the caller's zones; domain.ErrNotFound otherwise.
func (p *Privacy) DeleteZone(ctx context.Context, me domain.UserID, id domain.PrivacyZoneID) error {
	return p.Visibility.DeletePrivacyZone(ctx, id, me)
}
