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
	Rider    domain.UserID
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

type RouteMembership struct {
	Route  domain.RecordedRouteID
	Anchor bool
}

type RouteCandidate struct {
	Activity domain.ActivityID
	Route    domain.RecordedRouteID
	Track    domain.RecordedTrack
	Kind     domain.RecordedRouteKind
}

type RouteCandidates struct {
	// Exactly one of Anchors, MembersOf or Unassigned selects the relationship.
	Anchors    bool
	MembersOf  domain.RecordedRouteID
	Unassigned bool
	Rider      domain.UserID
}

type RecordedRouteWriter interface {
	Lock(context.Context) (bool, error)
	Next(context.Context) (RouteJob, error)
	Finish(context.Context, RouteJob) error
	Retry(context.Context, RouteJob) error
	Input(context.Context, domain.ActivityID) (RouteInput, error)
	Track(context.Context, domain.ActivityID) (RouteTrack, error)
	SaveTrack(context.Context, RouteTrack) error
	DeleteTrack(context.Context, domain.ActivityID) error
	Membership(context.Context, domain.ActivityID) (RouteMembership, error)
	Candidates(context.Context, domain.ActivityID, RouteCandidates) ([]RouteCandidate, error)
	Orphan(context.Context) (domain.RecordedRouteID, domain.ActivityID, error)
	Anchor(context.Context, domain.RecordedRouteID) (RouteCandidate, error)
	SetAnchor(context.Context, domain.RecordedRouteID, domain.ActivityID) error
	Create(context.Context, domain.ActivityID) (domain.RecordedRouteID, error)
	SetMatch(context.Context, domain.ActivityID, domain.RecordedRouteID, float64) error
	RemoveMatch(context.Context, domain.ActivityID) error
	RetainMatches(context.Context, domain.RecordedRouteID, []domain.ActivityID) error
}

type RecordedRouteQuery struct {
	Rider, Search, Sort        string
	Kind                       domain.RecordedRouteKind
	MinDistanceM, MaxDistanceM float64
	Unridden                   bool
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
