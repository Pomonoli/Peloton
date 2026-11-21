package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/opencairn-org/cairn-core/internal/auth"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type GarminSyncRepo struct {
	pool *pgxpool.Pool
	box  *auth.SecretBox
}

var _ port.GarminSyncRepo = (*GarminSyncRepo)(nil)

func NewGarminSyncRepo(pool *pgxpool.Pool, box *auth.SecretBox) *GarminSyncRepo {
	return &GarminSyncRepo{pool: pool, box: box}
}

func (r *GarminSyncRepo) encrypt(user domain.UserID, session domain.GarminSession) ([]byte, error) {
	b, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	return r.box.Encrypt(b, domain.GarminSecretAAD(user))
}

func (r *GarminSyncRepo) Connect(ctx context.Context, user domain.UserID, profile string, session domain.GarminSession, now time.Time) error {
	ciphertext, err := r.encrypt(user, session)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO garmin_connections (user_id, account_handle, session_encrypted, next_run)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET
			session_encrypted = EXCLUDED.session_encrypted, next_run = EXCLUDED.next_run,
			lease = NULL, lease_until = NULL, needs_key = false, failures = 0, last_error = ''
		WHERE garmin_connections.account_handle = EXCLUDED.account_handle`,
		user.UUID(), profile, ciphertext, now)
	if err == nil && tag.RowsAffected() == 0 {
		return port.ErrGarminAccount
	}
	return err
}

func (r *GarminSyncRepo) Disconnect(ctx context.Context, user domain.UserID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM garmin_connections WHERE user_id = $1`, user.UUID())
	return err
}

const garminColumns = `user_id, COALESCE(lease::text, ''), lease_until, window_start, watermark, cursor_day, next_run, last_sync,
	page_offset, history_complete, history_rides, summary_only, skipped, failures, last_error, needs_key`

func scanGarmin(row pgx.Row, tail ...any) (domain.GarminSync, error) {
	var s domain.GarminSync
	var uid uuid.UUID
	err := row.Scan(append([]any{&uid, &s.Lease, &s.LeaseUntil, &s.Window, &s.Watermark, &s.CursorDay, &s.NextRun, &s.LastSync,
		&s.Offset, &s.HistoryComplete, &s.HistoryRides, &s.SummaryOnly, &s.Skipped, &s.Failures, &s.Error, &s.NeedsKey}, tail...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	s.UserID = domain.UserID(uid)
	return s, err
}

func (r *GarminSyncRepo) Status(ctx context.Context, user domain.UserID) (domain.GarminSync, error) {
	return scanGarmin(r.pool.QueryRow(ctx, `SELECT `+garminColumns+` FROM garmin_connections WHERE user_id = $1`, user.UUID()))
}

func (r *GarminSyncRepo) Queue(ctx context.Context, user domain.UserID, now time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE garmin_connections SET next_run = CASE WHEN last_error = 'rate_limit' THEN next_run
			ELSE LEAST(next_run, $2) END
		WHERE user_id = $1 AND NOT needs_key`, user.UUID(), now)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

func (r *GarminSyncRepo) Claim(ctx context.Context, now, leaseUntil time.Time) (domain.GarminSync, domain.GarminSession, error) {
	var encrypted []byte
	s, err := scanGarmin(r.pool.QueryRow(ctx, `
		UPDATE garmin_connections SET lease = $1, lease_until = $2, window_start = COALESCE(window_start, $3)
		WHERE user_id = (
			SELECT user_id FROM garmin_connections
			WHERE next_run <= $3 AND NOT needs_key AND (lease_until IS NULL OR lease_until <= $3)
				AND EXISTS (SELECT 1 FROM users WHERE users.id = garmin_connections.user_id AND status = 'active')
			ORDER BY next_run, user_id LIMIT 1 FOR UPDATE SKIP LOCKED
		) RETURNING `+garminColumns+`, session_encrypted`, uuid.New(), leaseUntil, now), &encrypted)
	if err != nil {
		return s, domain.GarminSession{}, err
	}
	b, err := r.box.Decrypt(encrypted, domain.GarminSecretAAD(s.UserID))
	var session domain.GarminSession
	if err != nil || json.Unmarshal(b, &session) != nil {
		// Claim still succeeded. A new key can repair an unreadable session.
		return s, domain.GarminSession{}, nil
	}
	return s, session, nil
}

func (r *GarminSyncRepo) Guard(ctx context.Context, user domain.UserID, lease string) error {
	var ok bool
	err := dbtx(ctx, r.pool).QueryRow(ctx, `
		SELECT true FROM garmin_connections WHERE user_id = $1 AND lease = $2::uuid
		AND lease_until > clock_timestamp()
		AND EXISTS (SELECT 1 FROM users WHERE users.id = garmin_connections.user_id AND status = 'active') FOR UPDATE`, user.UUID(), lease).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func (r *GarminSyncRepo) Checkpoint(ctx context.Context, s domain.GarminSync, session *domain.GarminSession, release bool) error {
	var encrypted []byte
	if session != nil {
		var err error
		encrypted, err = r.encrypt(s.UserID, *session)
		if err != nil {
			return err
		}
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE garmin_connections SET window_start = $3, watermark = $4, cursor_day = $5, next_run = $6, last_sync = $7,
			page_offset = $8, history_complete = $9, history_rides = $10, summary_only = $11, skipped = $12,
			failures = $13, last_error = $14, needs_key = $15, session_encrypted = COALESCE($16, session_encrypted),
			lease = CASE WHEN $17 THEN NULL ELSE lease END, lease_until = CASE WHEN $17 THEN NULL ELSE lease_until END
		WHERE user_id = $1 AND lease = $2::uuid AND lease_until > now()`,
		s.UserID.UUID(), s.Lease, s.Window, s.Watermark, s.CursorDay, s.NextRun, s.LastSync,
		s.Offset, s.HistoryComplete, s.HistoryRides, s.SummaryOnly, s.Skipped,
		s.Failures, s.Error, s.NeedsKey, encrypted, release)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}
