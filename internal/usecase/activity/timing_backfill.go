package activity

import (
	"context"
	"errors"
	"fmt"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// TimingBackfill recalculates historical canonical timing and its gear totals.
type TimingBackfill struct {
	Activities       activityTimingStore
	ActivitiesToScan interface {
		ListActivityIDs(context.Context, []domain.UserID, domain.ActivityID, int) ([]domain.ActivityID, error)
	}
	Gear interface {
		ListUserIDsWithGear(context.Context) ([]domain.UserID, error)
		RecomputeTotalsForUser(context.Context, domain.UserID) error
	}
	Tx interface {
		port.TxManager
		port.UserLock
	}
	Streams             port.StreamRepo
	Overrides           port.ClassificationOverrideRepo
	Archive             port.ActivityTimingArchive
	Recompute           *RecomputeActivityFromSources
	RebuildTrainingLoad func(context.Context, domain.UserID) error
}

func (b TimingBackfill) Execute(ctx context.Context) (int, error) {
	const pageSize = 100
	var after domain.ActivityID
	var failures []error
	riders := make(map[domain.UserID]bool)
	updated := 0
	for {
		ids, err := b.ActivitiesToScan.ListActivityIDs(ctx, nil, after, pageSize)
		if err != nil {
			failures = append(failures, err)
			break
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return updated, errors.Join(append(failures, ctx.Err())...)
			}
			var changed bool
			err := b.Tx.InTx(ctx, func(ctx context.Context) error {
				act, err := b.Activities.GetActivity(ctx, id)
				if errors.Is(err, domain.ErrNotFound) {
					return nil
				}
				if err != nil {
					return err
				}
				if err := b.Tx.LockUser(ctx, act.UserID); err != nil {
					return err
				}
				riders[act.UserID] = true
				changed, err = b.Recompute.refreshActivityTiming(ctx, b.Activities, b.Streams, b.Overrides, b.Archive, id)
				return err
			})
			if err != nil {
				failures = append(failures, fmt.Errorf("backfill activity %s: %w", id, err))
			} else if changed {
				updated++
			}
		}
		if len(ids) < pageSize {
			break
		}
		after = ids[len(ids)-1]
	}
	for rider := range riders {
		if err := b.RebuildTrainingLoad(ctx, rider); err != nil {
			failures = append(failures, fmt.Errorf("rebuild training load for %s: %w", rider, err))
		}
	}
	// Refresh even on a retry with zero changes, so an interrupted run can
	// repair gear totals after it has already committed the activity updates.
	owners, err := b.Gear.ListUserIDsWithGear(ctx)
	if err != nil {
		return updated, errors.Join(append(failures, err)...)
	}
	for _, owner := range owners {
		err := b.Tx.InTx(ctx, func(ctx context.Context) error {
			if err := b.Tx.LockUser(ctx, owner); err != nil {
				return err
			}
			return b.Gear.RecomputeTotalsForUser(ctx, owner)
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("refresh gear totals for %s: %w", owner, err))
		}
	}
	return updated, errors.Join(failures...)
}
