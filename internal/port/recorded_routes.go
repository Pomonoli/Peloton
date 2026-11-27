package port

import (
	"context"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

type RouteJob struct {
	Activity   domain.ActivityID
	Generation int64
}

type RouteInput struct {
	Source   domain.SourceID
	Revision int64
	Shared   bool
}

type RouteTrack struct {
	Activity domain.ActivityID
	Source   domain.SourceID
	Revision int64
	LengthM  float64
	Kind     domain.RecordedRouteKind
	Track    domain.RecordedTrack
}

type RouteCandidate struct {
	Activity  domain.ActivityID
	Route     domain.RecordedRouteID
	Rider     domain.UserID
	StartTime time.Time
	Track     domain.RecordedTrack
}

type RouteMatch struct {
	Activity domain.ActivityID
	Overlap  float64
}

type RecordedRouteWriter interface {
	Lock(context.Context) (bool, error)
	EnqueueRecluster(context.Context) (bool, error)
	Next(context.Context) (RouteJob, error)
	Finish(context.Context, RouteJob) error
	Retry(context.Context, RouteJob) error
	Input(context.Context, domain.ActivityID) (RouteInput, error)
	Track(context.Context, domain.ActivityID) (RouteTrack, error)
	SaveTrack(context.Context, RouteTrack) error
	DeleteTrack(context.Context, domain.ActivityID) error
	Membership(context.Context, domain.ActivityID) (domain.RecordedRouteID, error)
	Candidate(context.Context, domain.ActivityID) (RouteCandidate, error)
	Nearby(ctx context.Context, refs, exclude []domain.ActivityID) ([]RouteCandidate, error)
	Members(context.Context, domain.RecordedRouteID) ([]RouteCandidate, error)
	Orphan(context.Context) (domain.RecordedRouteID, error)
	// SaveCluster keeps the first route ID, removes the rest, and queues any
	// released members. An empty match list removes the listed routes.
	SaveCluster(context.Context, []domain.RecordedRouteID, domain.ActivityID, []RouteMatch) error
}

type RecordedRouteQuery struct {
	Rider, Search, Sort        string
	Kind                       domain.RecordedRouteKind
	MinDistanceM, MaxDistanceM float64
	Unridden, Map              bool
	Offset, Limit              int
}

type RecordedRouteRider struct {
	UserID                           domain.UserID
	Username, DisplayName, AvatarURL string
	Rides                            int
	BestSpeedKmh                     *float64
}

type RecordedRouteRide struct {
	ID                  domain.ActivityID
	Rider               domain.UserID
	Title               string
	StartTime           time.Time
	Timezone            string
	DistanceM, SpeedKmh *float64
}

type RecordedRouteSummary struct {
	ID                domain.RecordedRouteID
	Owner             domain.UserID
	Name              string
	Kind              domain.RecordedRouteKind
	DistanceM         float64
	ElevationM        *float64
	Rides, RiderCount int
	BestSpeedKmh      *float64
	Track             domain.RecordedTrack
	Riders            []RecordedRouteRider
}

type RecordedRouteListing struct {
	Routes []RecordedRouteSummary
	Total  int
}

type RecordedRouteDetail struct {
	RecordedRouteSummary
	Recent []RecordedRouteRide
}

type RecordedRouteReader interface {
	List(context.Context, domain.UserID, RecordedRouteQuery) (RecordedRouteListing, error)
	Detail(context.Context, domain.UserID, domain.RecordedRouteID, int, int) (RecordedRouteDetail, error)
}
