package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func (r *MaintenanceRepo) LockTrainingStressBackfill(ctx context.Context) (port.TrainingStressBackfillState, bool, error) {
	var state port.TrainingStressBackfillState
	var owner, activity uuid.UUID
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT owner_cursor, activity_cursor, completed, scanned, repaired, missing, missing_with_duration
		FROM training_stress_backfill WHERE singleton FOR UPDATE SKIP LOCKED`,
	).Scan(&owner, &activity, &state.Completed, &state.Scanned, &state.Repaired, &state.Missing, &state.MissingWithDuration)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, false, nil
	}
	state.Owner, state.Activity = domain.UserID(owner), domain.ActivityID(activity)
	return state, err == nil, err
}

func (r *MaintenanceRepo) SaveTrainingStressBackfill(ctx context.Context, state port.TrainingStressBackfillState) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `
		UPDATE training_stress_backfill
		SET owner_cursor = $1, activity_cursor = $2, completed = $3,
		    scanned = $4, repaired = $5, missing = $6, missing_with_duration = $7
		WHERE singleton`, state.Owner.UUID(), state.Activity.UUID(), state.Completed,
		state.Scanned, state.Repaired, state.Missing, state.MissingWithDuration)
	return err
}

func (r *MaintenanceRepo) NextTrainingStressOwner(ctx context.Context, after domain.UserID) (domain.UserID, error) {
	var id uuid.UUID
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT u.id FROM users u WHERE u.id > $1
		AND EXISTS (SELECT 1 FROM activities a WHERE a.user_id = u.id AND a.deleted_at IS NULL)
		ORDER BY u.id LIMIT 1`, after.UUID()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UserID{}, nil
	}
	return domain.UserID(id), err
}

func (r *MaintenanceRepo) CountMissingTrainingStress(ctx context.Context) (missing, withMovingTime int64, err error) {
	err = dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE a.moving_duration_s > 0)
		FROM activities a WHERE a.deleted_at IS NULL AND `+missingTrainingStressSQL,
	).Scan(&missing, &withMovingTime)
	return missing, withMovingTime, err
}
