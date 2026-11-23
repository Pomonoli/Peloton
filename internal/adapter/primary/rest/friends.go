package rest

import (
	"net/http"

	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

type FriendsHandlers struct {
	Auth    *Auth
	Friends *view.Friends
}

func (h *FriendsHandlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/riders", h.list)
	mux.HandleFunc("GET /api/friends", h.list)
}

func (h *FriendsHandlers) list(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.Auth.requireUser(w, r)
	if !ok {
		return
	}
	members, err := h.Friends.List(r.Context(), viewer)
	if err != nil {
		writeActionError(w, err, "Unable to load the member directory")
		return
	}
	out := make([]map[string]any, 0, len(members))
	for _, member := range members {
		row := map[string]any{
			"id": member.UserID.String(), "username": member.Username, "display_name": member.DisplayName,
			"avatar_url": member.AvatarURL, "activity_count": member.ActivityCount, "cycling_distance_m": member.CyclingDistanceM,
			"latest_ride": nil,
		}
		if ride := member.LatestRide; ride != nil {
			row["latest_ride"] = map[string]any{"id": ride.ID.String(), "title": ride.Title, "start_time": ride.StartTime}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}
