package port

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// ReportRepo persists content reports for the moderation queue (migration 36).
type ReportRepo interface {
	Create(ctx context.Context, r domain.ContentReport) (domain.ContentReportID, error)
	List(ctx context.Context, status domain.ReportStatus, limit, offset int) ([]domain.ContentReport, error)
	UpdateStatus(ctx context.Context, id domain.ContentReportID, status domain.ReportStatus, reviewer domain.UserID) error
}

// ModerationRepo is admin content actions not tied to a single aggregate.
type ModerationRepo interface {
	// SetActivityHidden returns domain.ErrNotFound for an unknown activity.
	SetActivityHidden(ctx context.Context, activityID domain.ActivityID, hidden bool) error
}
