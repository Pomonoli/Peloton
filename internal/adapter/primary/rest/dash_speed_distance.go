package rest

import (
	"net/http"
	"strconv"
	"time"

	"github.com/opencairn-org/cairn-core/internal/port"
)

func (h *DashboardHandlers) speedDistance(w http.ResponseWriter, r *http.Request) {
	ctx, rider, ok := h.Auth.requireMember(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	query := r.URL.Query()
	var year int
	if raw := query.Get("year"); raw != "" {
		var err error
		year, err = strconv.Atoi(raw)
		if err != nil || year < 1 || year > 9999 {
			writeActionError(w, port.NewActionError(port.ActionErrorInvalid, "Invalid Stats year"), "")
			return
		}
	}
	points, err := h.SpeedDistance.ListSpeedDistance(ctx, rider, dashActivityFilter(query), year)
	if err != nil {
		h.Log.Debug("load speed versus distance", "error", err)
		http.Error(w, "Unable to load speed versus distance", http.StatusInternalServerError)
		return
	}
	type point struct {
		ID         string    `json:"id"`
		Title      string    `json:"title"`
		Start      time.Time `json:"start"`
		DistanceKm float64   `json:"distance_km"`
		SpeedKmh   float64   `json:"speed_kmh"`
	}
	out := make([]point, 0, len(points))
	for _, p := range points {
		out = append(out, point{p.ID.String(), p.Title, p.Start, p.DistanceM / 1000, p.SpeedMps * 3.6})
	}
	writeJSON(w, http.StatusOK, out)
}
