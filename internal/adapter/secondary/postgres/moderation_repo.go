package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// ReportRepo implements port.ReportRepo over content_reports.
type ReportRepo struct{ pool *pgxpool.Pool }

func NewReportRepo(pool *pgxpool.Pool) *ReportRepo { return &ReportRepo{pool: pool} }

func (r *ReportRepo) Create(ctx context.Context, rep domain.ContentReport) (domain.ContentReportID, error) {
	db := dbtx(ctx, r.pool)
	var id uuid.UUID
	err := db.QueryRow(ctx,
		`INSERT INTO content_reports (reporter_id, target_kind, target_id, reason)
		 VALUES ($1,$2,$3,$4) RETURNING id`,
		rep.ReporterID.UUID(), string(rep.TargetKind), rep.TargetID, rep.Reason).Scan(&id)
	if err != nil {
		return domain.ContentReportID{}, fmt.Errorf("create report: %w", err)
	}
	return domain.ContentReportID(id), nil
}

func (r *ReportRepo) List(ctx context.Context, status domain.ReportStatus, limit, offset int) ([]domain.ContentReport, error) {
	if limit <= 0 {
		limit = 50
	}
	db := dbtx(ctx, r.pool)
	q := `SELECT id, reporter_id, target_kind, target_id, reason, status, created_at, reviewed_at, reviewed_by
	        FROM content_reports`
	args := []any{}
	if status != "" {
		q += ` WHERE status = $1`
		args = append(args, string(status))
	}
	args = append(args, limit, offset)
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	defer rows.Close()
	var out []domain.ContentReport
	for rows.Next() {
		var (
			rep          domain.ContentReport
			id, reporter uuid.UUID
			targetID     uuid.UUID
			kind, st     string
			reviewedBy   *uuid.UUID
		)
		if err := rows.Scan(&id, &reporter, &kind, &targetID, &rep.Reason, &st,
			&rep.CreatedAt, &rep.ReviewedAt, &reviewedBy); err != nil {
			return nil, err
		}
		rep.ID = domain.ContentReportID(id)
		rep.ReporterID = domain.UserID(reporter)
		rep.TargetKind = domain.ReportTargetKind(kind)
		rep.TargetID = targetID
		rep.Status = domain.ReportStatus(st)
		if reviewedBy != nil {
			uid := domain.UserID(*reviewedBy)
			rep.ReviewedBy = &uid
		}
		out = append(out, rep)
	}
	return out, rows.Err()
}

func (r *ReportRepo) UpdateStatus(ctx context.Context, id domain.ContentReportID, status domain.ReportStatus, reviewer domain.UserID) error {
	db := dbtx(ctx, r.pool)
	_, err := db.Exec(ctx,
		`UPDATE content_reports SET status=$2, reviewed_at=now(), reviewed_by=$3 WHERE id=$1`,
		id.UUID(), string(status), reviewer.UUID())
	if err != nil {
		return fmt.Errorf("update report status: %w", err)
	}
	return nil
}

// ModerationRepo implements port.ModerationRepo.
type ModerationRepo struct{ pool *pgxpool.Pool }

func NewModerationRepo(pool *pgxpool.Pool) *ModerationRepo { return &ModerationRepo{pool: pool} }

func (r *ModerationRepo) SetActivityHidden(ctx context.Context, activityID domain.ActivityID, hidden bool) error {
	db := dbtx(ctx, r.pool)
	tag, err := db.Exec(ctx, `UPDATE activities SET hidden_by_admin=$2 WHERE id=$1`, activityID.UUID(), hidden)
	if err != nil {
		return fmt.Errorf("set activity hidden: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
