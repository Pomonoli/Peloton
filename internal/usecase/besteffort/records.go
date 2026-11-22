package besteffort

import (
	"cmp"
	"context"
	"slices"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// CommunityRecords reads the sliding-window results used by personal records.
type CommunityRecords struct {
	Reader port.CommunityRecordReader
}

func (s *CommunityRecords) List(ctx context.Context, viewer domain.UserID) ([]domain.CommunityRecord, error) {
	if viewer == (domain.UserID{}) {
		return nil, port.NewActionError(port.ActionErrorForbidden, "Authentication required")
	}
	rows, err := s.Reader.ListCommunityRecords(ctx, nil)
	if err != nil {
		return nil, err
	}
	types := map[domain.ActivityType]bool{domain.ActivityTypeRide: true}
	seen := make(map[domain.RecordBucket]bool, len(rows))
	for _, row := range rows {
		types[row.Bucket.ActivityType] = true
		seen[row.Bucket] = true
	}
	for activityType := range types {
		for _, bucket := range StandardRecordBuckets(activityType) {
			if !seen[bucket] {
				rows = append(rows, domain.CommunityRecord{Bucket: bucket})
			}
		}
	}
	slices.SortFunc(rows, func(a, b domain.CommunityRecord) int {
		return CompareRecordBuckets(a.Bucket, b.Bucket)
	})
	return rows, nil
}

func (s *CommunityRecords) Detail(ctx context.Context, viewer domain.UserID, bucket domain.RecordBucket) ([]domain.CommunityRecord, error) {
	if viewer == (domain.UserID{}) {
		return nil, port.NewActionError(port.ActionErrorForbidden, "Authentication required")
	}
	if !bucket.Valid() {
		return nil, port.NewActionError(port.ActionErrorInvalid, "Invalid record category or window")
	}
	return s.Reader.ListCommunityRecords(ctx, &bucket)
}

// StandardRecordBuckets returns the record categories computed for an activity type.
func StandardRecordBuckets(activityType domain.ActivityType) []domain.RecordBucket {
	windows := standardWindowsFor(activityType)
	buckets := make([]domain.RecordBucket, 0, len(windows))
	for _, w := range windows {
		buckets = append(buckets, domain.RecordBucket{
			ActivityType: activityType, Metric: w.Metric, WindowKind: w.Kind, WindowValue: w.Value,
		})
	}
	return buckets
}

func CompareRecordBuckets(a, b domain.RecordBucket) int {
	return cmp.Or(cmp.Compare(a.ActivityType, b.ActivityType), cmp.Compare(a.Metric, b.Metric),
		cmp.Compare(a.WindowKind, b.WindowKind), cmp.Compare(a.WindowValue, b.WindowValue))
}
