package besteffort

import (
	"context"
	"sort"

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
		for _, window := range StandardWindows {
			bucket := domain.RecordBucket{ActivityType: activityType, Metric: window.Metric, WindowKind: window.Kind, WindowValue: window.Value}
			if !seen[bucket] {
				rows = append(rows, domain.CommunityRecord{Bucket: bucket})
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i].Bucket, rows[j].Bucket
		if a.ActivityType != b.ActivityType {
			return a.ActivityType < b.ActivityType
		}
		if a.Metric != b.Metric {
			return a.Metric < b.Metric
		}
		if a.WindowKind != b.WindowKind {
			return a.WindowKind < b.WindowKind
		}
		return a.WindowValue < b.WindowValue
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
