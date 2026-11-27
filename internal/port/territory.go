package port

import (
	"context"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/territory"
)

const TerritoryWindow = 90 * 24 * time.Hour

type TerritoryRide struct {
	Activity domain.ActivityID
	Source   domain.SourceID
	Revision int64
}

type TerritoryRider struct {
	domain.CommunityMember
	ColorIndex int
	Zones      []domain.PrivacyZone
}

type TerritoryScore struct {
	Cell       territory.Cell
	Rider      domain.UserID
	DistanceMM int64
}

// TerritoryRead is a consistent view of the eligible activity versions and
// current privacy rules. Scores and riders are only loaded on a cache miss.
type TerritoryRead struct {
	Version string
	Pending int
	Rides   []TerritoryRide
	Riders  []TerritoryRider
	Scores  []TerritoryScore
}

type TerritoryRepo interface {
	ReadTerritory(ctx context.Context, now time.Time, knownVersion string) (TerritoryRead, error)
	SaveTerritory(ctx context.Context, ride TerritoryRide, distances map[territory.Cell]int64) error
}
