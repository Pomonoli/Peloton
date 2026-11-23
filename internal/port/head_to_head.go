package port

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// Comparison readers apply activity and segment visibility before picking
// either rider's best eligible efforts.
type SegmentComparisonReader interface {
	ListSharedSegments(ctx context.Context, viewer, first, second domain.UserID) ([]domain.SharedSegment, error)
}

type RecordComparisonReader interface {
	ListComparisonRecords(ctx context.Context, viewer, first, second domain.UserID) ([]domain.MemberRecord, error)
}
