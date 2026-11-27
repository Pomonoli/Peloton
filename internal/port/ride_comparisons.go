package port

import (
	"context"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

type SpeedDistancePoint struct {
	ID                  domain.ActivityID
	Title               string
	Start               time.Time
	DistanceM, SpeedMps float64
}

type SpeedDistanceReader interface {
	ListSpeedDistance(ctx context.Context, rider domain.UserID, filter ActivityListFilter, year int) ([]SpeedDistancePoint, error)
}

type SharedRideRecording struct {
	ID         domain.ActivityID
	Source     domain.SourceID
	Title      string
	Start, End time.Time
	Revision   int64
}

type SharedRidePair struct {
	First, Second SharedRideRecording
	Signature     string
	Cached        *domain.RideProximity
}

type SharedRideReader interface {
	ListSharedRidePairs(ctx context.Context, viewer, first, second domain.UserID, from, to time.Time, visibility int16, maskKey string) ([]SharedRidePair, error)
	ReadSharedRideTrack(ctx context.Context, recording SharedRideRecording) ([]domain.StreamSample, error)
	SaveSharedRidePair(ctx context.Context, pair SharedRidePair, visibility int16, proximity domain.RideProximity) error
}
