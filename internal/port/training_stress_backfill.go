package port

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// TrainingStressBackfillState checkpoints work in owner and activity order.
// A zero Activity means the owner has already been completed.
type TrainingStressBackfillState struct {
	Owner               domain.UserID
	Activity            domain.ActivityID
	Completed           bool
	Scanned             int64
	Repaired            int64
	Missing             int64
	MissingWithDuration int64
}

type TrainingStressBackfillRepo interface {
	// LockTrainingStressBackfill skips the current batch when another server
	// is processing it. The lock and checkpoint use the same transaction.
	LockTrainingStressBackfill(context.Context) (TrainingStressBackfillState, bool, error)
	SaveTrainingStressBackfill(context.Context, TrainingStressBackfillState) error
	NextTrainingStressOwner(context.Context, domain.UserID) (domain.UserID, error)
	CountMissingTrainingStress(context.Context) (missing, withMovingTime int64, err error)
	ListActivityIDs(context.Context, []domain.UserID, domain.ActivityID, int) ([]domain.ActivityID, error)
}
