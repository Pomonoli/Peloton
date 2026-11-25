package rest

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/sharing"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

// ShareHandlers serve activity share links.
//
//	POST   /api/activities/{id}/share   owner: mint an unguessable link
//	GET    /api/activities/{id}/shares  owner: list links
//	DELETE /api/shares/{token}          owner: revoke
//	GET    /api/shared/{token}          authenticated: read-only projected activity
type ShareHandlers struct {
	Auth   *Auth
	Links  *sharing.Links
	Shares *view.ActivityShares
	Log    *slog.Logger
}

// Mount registers the routes.
func (h *ShareHandlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/activities/{id}/share", h.create)
	mux.HandleFunc("GET /api/activities/{id}/shares", h.list)
	mux.HandleFunc("DELETE /api/shares/{token}", h.revoke)
	mux.HandleFunc("GET /api/shared/{token}", h.read)
}

// ownedActivity resolves the caller and the {id} path value or writes an error.
func (h *ShareHandlers) ownedActivity(w http.ResponseWriter, r *http.Request) (domain.UserID, domain.ActivityID, bool) {
	me, ok := h.Auth.requireUser(w, r)
	if !ok {
		return me, domain.ActivityID{}, false
	}
	actID, err := domain.ParseUUID[domain.ActivityID](r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return me, actID, false
	}
	return me, actID, true
}

func (h *ShareHandlers) create(w http.ResponseWriter, r *http.Request) {
	me, actID, ok := h.ownedActivity(w, r)
	if !ok {
		return
	}
	token, err := h.Links.Create(r.Context(), me, actID)
	if err != nil {
		writeActionError(w, err, "create failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"path":  "/shared/" + token,
	})
}

func (h *ShareHandlers) list(w http.ResponseWriter, r *http.Request) {
	me, actID, ok := h.ownedActivity(w, r)
	if !ok {
		return
	}
	links, err := h.Links.List(r.Context(), me, actID)
	if err != nil {
		writeActionError(w, err, "load failed")
		return
	}
	out := make([]map[string]any, 0, len(links))
	for _, l := range links {
		out = append(out, map[string]any{
			"token":      l.Token,
			"path":       "/shared/" + l.Token,
			"created_at": l.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			"active":     l.Active(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

func (h *ShareHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	me, ok := h.Auth.requireUser(w, r)
	if !ok {
		return
	}
	if err := h.Links.Revoke(r.Context(), me, r.PathValue("token")); err != nil {
		writeActionError(w, err, "revoke failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ShareHandlers) read(w http.ResponseWriter, r *http.Request) {
	me, ok := h.Auth.requireUser(w, r)
	if !ok {
		return
	}
	p, owner, err := h.Shares.Shared(r.Context(), h.Links.Repo, r.PathValue("token"), &me)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "link not found or revoked", http.StatusNotFound)
			return
		}
		writeActionError(w, err, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"activity": projectedJSON(p),
		"owner":    userCardJSON(owner),
		"shared":   true,
	})
}
