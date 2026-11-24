package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/opencairn-org/cairn-core/internal/adapter/primary/rest"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
	"github.com/opencairn-org/cairn-core/internal/usecase/besteffort"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

// mountRestDash mounts the owner dashboards, records and the review queue.
func mountRestDash(mux *http.ServeMux, app *App, logger *slog.Logger) {
	auth := restAuth(app)
	(&rest.DashboardHandlers{Auth: auth, Overviews: app.Overviews, Searches: app.Searches, Stats: app.Stats, PerformanceMaps: app.PerformanceMaps, Visibility: app.Visibility2, Log: logger}).Mount(mux)
	(&rest.ReviewHandlers{Auth: auth, Queue: &activity.ReviewQueue{Activities: app.Activities}, Links: app.SourceLinks, Log: logger}).Mount(mux)
	if app.BestEfforts != nil {
		(&rest.RecordHandlers{Auth: auth, Stats: app.Stats, Community: &besteffort.CommunityRecords{Reader: app.BestEfforts}, Log: logger}).Mount(mux)
	}
	if app.Users != nil && app.Blocks != nil && app.Segments != nil && app.BestEfforts != nil {
		(&rest.HeadToHeadHandlers{Auth: auth, Log: logger, Views: &view.HeadToHead{
			Members: auth.Members, Blocks: app.Blocks, Segments: app.Segments, Records: app.BestEfforts,
		}}).Mount(mux)
	}
	if app.CustomBestEfforts != nil {
		(&rest.CustomRecordHandlers{
			Auth: auth, Customs: app.CustomBestEfforts,
			Backfill: func(userID domain.UserID, at domain.ActivityType) { backfillCustomRecords(app, logger, userID, at) },
		}).Mount(mux)
	}
	logger.Info("dashboard endpoints mounted")
}

// backfillCustomRecords recomputes the user's efforts of one type in the
// background so the request returns at once; the outcome is logged.
func backfillCustomRecords(app *App, logger *slog.Logger, userID domain.UserID, at domain.ActivityType) {
	goSafe(logger, "custom best-effort backfill", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		sources, failed, err := app.CustomBestEfforts.Backfill(ctx, userID, at)
		logger.Info("custom best-effort backfill finished", "user_id", userID, "activity_type", at,
			"sources", sources, "failed", failed, "error", err)
	})
}
