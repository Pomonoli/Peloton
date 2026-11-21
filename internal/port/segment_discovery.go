package port

import (
	"context"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// SegmentDiscoveryProvider looks up Strava cycling segment definitions using WGS84 bounds.
type SegmentDiscoveryProvider interface {
	Name() string
	Discover(ctx context.Context, box BoundingBox) (SegmentDiscoveryPage, error)
}

type SegmentDiscoveryPage struct {
	Segments  []domain.Segment
	Saturated bool
}

// SegmentDiscoveryWindow identifies a cell in the discovery grid.
type SegmentDiscoveryWindow struct {
	X, Y, Depth int
}

type SegmentDiscoveryCache interface {
	SaveSegment(ctx context.Context, segment domain.Segment) error
	DiscoveryWindow(ctx context.Context, provider string, window SegmentDiscoveryWindow, since time.Time) (fresh, saturated bool, err error)
	SaveDiscoveryWindow(ctx context.Context, provider string, window SegmentDiscoveryWindow, fetchedAt time.Time, saturated bool) error
}

// SegmentMatchLocker serializes stream reads and effort replacement for one
// source when automatic matching and rescans overlap.
type SegmentMatchLocker interface {
	LockSegmentMatch(ctx context.Context, id domain.SourceID) error
}
