package besteffort

import (
	"context"
	"errors"
	"fmt"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// BackfillCyclingDistances scans a small page of recorded rides at a time.
// It runs only the added distance windows and can be restarted safely.
func (uc *ComputeBestEffortsForSource) BackfillCyclingDistances(ctx context.Context, store port.CyclingRecordBackfillStore) (int, error) {
	const pageSize = 100
	var cursor domain.SourceID
	var failures []error
	count := 0
	for {
		sources, err := store.ListCyclingSourcesForRecordBackfill(ctx, cursor, pageSize)
		if err != nil {
			return count, errors.Join(append(failures, err)...)
		}
		for _, id := range sources {
			if ctx.Err() != nil {
				return count, errors.Join(append(failures, ctx.Err())...)
			}
			if err := uc.backfillCyclingSource(ctx, store, id); err != nil {
				failures = append(failures, fmt.Errorf("backfill source %s: %w", id, err))
			} else {
				count++
			}
			cursor = id
		}
		if len(sources) < pageSize {
			return count, errors.Join(failures...)
		}
	}
}

func (uc *ComputeBestEffortsForSource) backfillCyclingSource(ctx context.Context, store port.CyclingRecordBackfillStore, id domain.SourceID) error {
	src, err := uc.activities.GetSource(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	act, err := uc.activities.GetActivity(ctx, src.ActivityID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if act.Type != domain.ActivityTypeRide || act.IsDeleted() || src.Status == domain.SourceStatusDetached {
		return nil
	}
	stream, err := uc.streams.QueryStream(ctx, domain.StreamQuery{
		ActivitySourceID: id,
		Resolution:       domain.StreamResolutionRaw,
		Channels:         []domain.StreamChannel{domain.StreamChannelDistance},
	})
	if err != nil {
		return err
	}
	efforts := uc.computeAll(act, src, stream.Samples, longCyclingWindows)
	return store.SaveMissingEfforts(ctx, efforts)
}
