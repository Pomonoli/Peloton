package rest

import (
	"context"
	"net/http"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

func (a *Auth) requireMember(w http.ResponseWriter, r *http.Request) (context.Context, domain.UserID, bool) {
	viewer, ok := a.requireUser(w, r)
	if !ok {
		return r.Context(), viewer, false
	}
	ctx, member, err := a.Members.Read(r.Context(), viewer, r.URL.Query().Get("member"))
	if err != nil {
		writeActionError(w, err, "Unable to load the member")
		return ctx, member, false
	}
	return ctx, member, true
}

func (a *Auth) MountMembers(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/members/{member}", func(w http.ResponseWriter, r *http.Request) {
		viewer, ok := a.requireUser(w, r)
		if !ok {
			return
		}
		member, err := a.Members.Get(r.Context(), viewer, r.PathValue("member"))
		if err != nil {
			writeActionError(w, err, "Unable to load the member")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"id": member.ID.String(), "username": member.Username, "display_name": member.DisplayName,
		})
	})
}
