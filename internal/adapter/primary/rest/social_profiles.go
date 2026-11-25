package rest

import (
	"errors"
	"net/http"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/social"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

// ProfileHandlers serve member profiles and privacy zones.
type ProfileHandlers struct {
	Auth     *Auth
	Profiles *view.Profiles
	Shares   *view.ActivityShares
	Privacy  *social.Privacy
}

// Mount registers the profile routes.
func (h *ProfileHandlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/profiles/{username}", h.getProfile)
	mux.HandleFunc("GET /api/activities/{id}/projected", h.projected)
}

// MountPrivacyZones registers the owner-only location masking settings.
func (h *ProfileHandlers) MountPrivacyZones(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/profile/privacy-zones", h.listZones)
	mux.HandleFunc("POST /api/profile/privacy-zones", h.addZone)
	mux.HandleFunc("DELETE /api/profile/privacy-zones/{id}", h.deleteZone)
}

func (h *ProfileHandlers) getProfile(w http.ResponseWriter, r *http.Request) {
	me, ok := h.Auth.requireUser(w, r)
	if !ok {
		return
	}
	pv, err := h.Profiles.GetPage(r.Context(), r.PathValue("username"), &me, parseIntQuery(r, "offset", 0))
	if err != nil {
		writeActionError(w, err, "load failed")
		return
	}
	acts := make([]map[string]any, 0, len(pv.Activities))
	for _, p := range pv.Activities {
		acts = append(acts, projectedJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":       userCardJSON(pv.User),
		"is_self":    pv.IsOwner,
		"activities": acts,
		"has_more":   pv.HasMore,
	})
}

func (h *ProfileHandlers) listZones(w http.ResponseWriter, r *http.Request) {
	me, ok := h.Auth.requireUser(w, r)
	if !ok {
		return
	}
	zones, err := h.Privacy.Zones(r.Context(), me)
	if err != nil {
		http.Error(w, "load failed", http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(zones))
	for _, z := range zones {
		out = append(out, map[string]any{
			"id": z.ID.String(), "label": z.Label,
			"lat": z.Lat, "lng": z.Lng, "radius_m": z.RadiusM,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"zones": out})
}

func (h *ProfileHandlers) addZone(w http.ResponseWriter, r *http.Request) {
	me, ok := h.Auth.requireUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Label   string  `json:"label"`
		Lat     float64 `json:"lat"`
		Lng     float64 `json:"lng"`
		RadiusM float64 `json:"radius_m"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	id, err := h.Privacy.AddZone(r.Context(), me, domain.PrivacyZone{
		Label: body.Label, Lat: body.Lat, Lng: body.Lng, RadiusM: body.RadiusM,
	})
	if err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id.String()})
}

func (h *ProfileHandlers) deleteZone(w http.ResponseWriter, r *http.Request) {
	me, ok := h.Auth.requireUser(w, r)
	if !ok {
		return
	}
	id, err := domain.ParseUUID[domain.PrivacyZoneID](r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := h.Privacy.DeleteZone(r.Context(), me, id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProfileHandlers) projected(w http.ResponseWriter, r *http.Request) {
	me, ok := h.Auth.requireUser(w, r)
	if !ok {
		return
	}
	actID, err := domain.ParseUUID[domain.ActivityID](r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	p, owner, isOwner, err := h.Shares.Projected(r.Context(), actID, &me, false)
	if err != nil {
		writeActionError(w, err, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"activity": projectedJSON(p),
		"owner":    userCardJSON(owner),
		"is_owner": isOwner,
	})
}
