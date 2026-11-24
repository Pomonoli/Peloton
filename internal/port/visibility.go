package port

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// PrivacyZoneRepo stores geographic masks for shared activity tracks.
type PrivacyZoneRepo interface {
	ListPrivacyZones(ctx context.Context, userID domain.UserID) ([]domain.PrivacyZone, error)
	AddPrivacyZone(ctx context.Context, z domain.PrivacyZone) (domain.PrivacyZoneID, error)
	DeletePrivacyZone(ctx context.Context, id domain.PrivacyZoneID, userID domain.UserID) error
}
