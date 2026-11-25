package main

import (
	"log/slog"
	"net/http"

	"github.com/opencairn-org/cairn-core/internal/adapter/primary/rest"
	"github.com/opencairn-org/cairn-core/internal/usecase/moderation"
	"github.com/opencairn-org/cairn-core/internal/usecase/social"
)

// mountRestSocial mounts member profiles, share links, moderation and quota.
func mountRestSocial(mux *http.ServeMux, app *App, logger *slog.Logger) {
	auth := restAuth(app)
	mountRestProfiles(mux, app, auth, logger)
	if app.FriendViews != nil {
		(&rest.FriendsHandlers{Auth: auth, Friends: app.FriendViews}).Mount(mux)
	}
	mountRestShare(mux, app, logger)
	mountRestModeration(mux, app, auth, logger)
	if app.Quotas != nil {
		(&rest.QuotaHandlers{Auth: auth, Quota: app.QuotaViews, UserAdmin: app.UserAdmin}).Mount(mux)
		logger.Info("quota endpoints mounted")
	}
}

func mountRestProfiles(mux *http.ServeMux, app *App, auth *rest.Auth, logger *slog.Logger) {
	if app.Users == nil {
		return
	}
	h := &rest.ProfileHandlers{Auth: auth, Profiles: app.ProfileViews, Shares: app.ActivityShares}
	h.Mount(mux)
	auth.MountMembers(mux)
	if app.Visibility != nil {
		h.Privacy = &social.Privacy{Visibility: app.Visibility}
		h.MountPrivacyZones(mux)
	}
	logger.Info("profile endpoints mounted")
}

func mountRestModeration(mux *http.ServeMux, app *App, auth *rest.Auth, logger *slog.Logger) {
	if app.Reports == nil {
		return
	}
	reports := app.ModReports
	if reports == nil {
		reports = &moderation.Reports{Reports: app.Reports}
	}
	h := &rest.ModerationHandlers{Auth: auth, Reports: reports, UserAdmin: app.UserAdmin}
	h.MountReports(mux)
	if app.ModReports != nil {
		h.MountAdmin(mux)
		if app.Moderation != nil {
			h.MountHide(mux)
		}
	}
	logger.Info("moderation endpoints mounted")
}
