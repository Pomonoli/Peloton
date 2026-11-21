package port

import (
	"context"
	"errors"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

var (
	ErrGarminUnauthorized = errors.New("Garmin rejected the credentials")
	ErrGarminIgnored      = errors.New("this Garmin source was removed by the user")
	ErrGarminNoFile       = errors.New("Garmin has no supported original activity file")
	ErrGarminAccount      = errors.New("a different Garmin account is already connected")
	ErrGarminInvalidRide  = errors.New("Garmin ride is missing valid activity data")
)

type GarminRateLimit struct{ RetryAfter time.Duration }

func (*GarminRateLimit) Error() string { return "Garmin rate limited the request" }

type GarminConnect interface {
	ExchangeKey(ctx context.Context, key string) (domain.GarminSession, error)
	ProfileHandle(ctx context.Context, token string) (string, error)
	Rides(ctx context.Context, token string, from, through time.Time, offset, limit int) ([]domain.GarminActivity, error)
	Original(ctx context.Context, token, externalID string) (UploadInput, error)
}

type GarminSyncRepo interface {
	Connect(ctx context.Context, user domain.UserID, profile string, session domain.GarminSession, now time.Time) error
	Disconnect(ctx context.Context, user domain.UserID) error
	Status(ctx context.Context, user domain.UserID) (domain.GarminSync, error)
	Queue(ctx context.Context, user domain.UserID, now time.Time) error
	Claim(ctx context.Context, now, leaseUntil time.Time) (domain.GarminSync, domain.GarminSession, error)
	// Guard locks the connection through the caller's ingest transaction.
	Guard(ctx context.Context, user domain.UserID, lease string) error
	Checkpoint(ctx context.Context, sync domain.GarminSync, session *domain.GarminSession, release bool) error
}

type GarminRideIngester interface {
	// Existing distinguishes already-ingested rides with and without a stream.
	Existing(ctx context.Context, user domain.UserID, externalID string) (exists, hasStream bool, err error)
	IngestRide(ctx context.Context, sync domain.GarminSync, ride domain.GarminActivity, file UploadInput) (hasStream bool, err error)
}
