package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
)

const trainingStressBatchSize = 100

// TrainingStressBackfill restores historical scores in bounded transactions.
// The score updates and their checkpoint commit together, so interruptions
// resume from the last committed batch, including at the history rebuild.
type TrainingStressBackfill struct {
	Repo port.TrainingStressBackfillRepo
	Tx   interface {
		port.TxManager
		port.UserLock
	}
	Repair  func(context.Context, domain.ActivityID) (activity.TrainingStressRepairResult, error)
	Rebuild func(context.Context, domain.UserID) error
}

func (b TrainingStressBackfill) Run(ctx context.Context, log *slog.Logger) {
	for ctx.Err() == nil {
		state, advanced, err := b.Step(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			log.Warn("training stress backfill batch failed", "error", err)
		case advanced && state.Completed:
			log.Info("training stress backfill completed", "scanned", state.Scanned, "repaired", state.Repaired,
				"remaining_missing", state.Missing, "missing_with_moving_time", state.MissingWithDuration)
			if state.MissingWithDuration > 0 {
				log.Warn("activities with moving time still lack a training stress score", "count", state.MissingWithDuration)
			}
		case advanced:
			log.Info("training stress backfill progress", "owner", state.Owner, "scanned", state.Scanned,
				"repaired", state.Repaired, "missing_in_processed_activities", state.Missing,
				"missing_with_moving_time", state.MissingWithDuration)
		}
		if state.Completed && err == nil {
			return
		}
		if err != nil || !advanced {
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
		}
	}
}

// Step processes one owner batch. Concurrent processes can attempt a batch,
// but only one can acquire the checkpoint row and update activities.
func (b TrainingStressBackfill) Step(ctx context.Context) (state port.TrainingStressBackfillState, advanced bool, err error) {
	err = b.Tx.InTx(ctx, func(ctx context.Context) error {
		var locked bool
		var err error
		state, locked, err = b.Repo.LockTrainingStressBackfill(ctx)
		if err != nil || !locked || state.Completed {
			return err
		}
		advanced = true
		if state.Activity == (domain.ActivityID{}) {
			next, err := b.Repo.NextTrainingStressOwner(ctx, state.Owner)
			if err != nil {
				return err
			}
			if next == (domain.UserID{}) {
				state.Missing, state.MissingWithDuration, err = b.Repo.CountMissingTrainingStress(ctx)
				if err != nil {
					return err
				}
				state.Completed = true
				return b.Repo.SaveTrainingStressBackfill(ctx, state)
			}
			state.Owner = next
		}
		if err := b.repairOwnerBatch(ctx, &state); err != nil {
			return err
		}
		return b.Repo.SaveTrainingStressBackfill(ctx, state)
	})
	return state, advanced && err == nil, err
}

func (b TrainingStressBackfill) repairOwnerBatch(ctx context.Context, state *port.TrainingStressBackfillState) error {
	if err := b.Tx.LockUser(ctx, state.Owner); err != nil {
		return err
	}
	ids, err := b.Repo.ListActivityIDs(ctx, []domain.UserID{state.Owner}, state.Activity, trainingStressBatchSize)
	if err != nil {
		return err
	}
	for _, id := range ids {
		result, err := b.Repair(ctx, id)
		if err != nil {
			return fmt.Errorf("repair training stress for %s: %w", id, err)
		}
		state.Scanned++
		if result.Changed {
			state.Repaired++
		}
		if result.Missing {
			state.Missing++
		}
		if result.MissingWithDuration {
			state.MissingWithDuration++
		}
		state.Activity = id
	}
	if len(ids) < trainingStressBatchSize {
		if err := b.Rebuild(ctx, state.Owner); err != nil {
			return fmt.Errorf("rebuild training load for %s: %w", state.Owner, err)
		}
		state.Activity = domain.ActivityID{}
	}
	return nil
}
