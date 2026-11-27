package rest

import (
	"math"
	"net/http"
	"strconv"
	"time"
)

func (h *DashboardHandlers) territoryHeatmap(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.Auth.requireUser(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	inspect := query.Has("lat") || query.Has("lon")
	var lat, lon float64
	if inspect {
		var latErr, lonErr error
		lat, latErr = strconv.ParseFloat(query.Get("lat"), 64)
		lon, lonErr = strconv.ParseFloat(query.Get("lon"), 64)
		if latErr != nil || lonErr != nil || math.IsNaN(lat) || math.IsNaN(lon) ||
			math.Abs(lat) > 90 || math.Abs(lon) > 180 {
			http.Error(w, "Invalid territory location", http.StatusBadRequest)
			return
		}
	}
	data, err := h.TerritoryMaps.Get(r.Context(), viewer, time.Now())
	if err != nil {
		h.Log.Warn("load territory heatmap", "error", err)
		writeActionError(w, err, "Unable to load territory")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if data.Pending > 0 {
		writeJSON(w, http.StatusAccepted, map[string]int{"pending": data.Pending})
		return
	}
	if inspect {
		distances, err := data.At(lat, lon)
		if err != nil {
			writeActionError(w, err, "Unable to inspect territory")
			return
		}
		if len(distances) == 0 {
			http.Error(w, "This hexagon is unclaimed", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"owner_id": distances[0].RiderID, "distances": distances, "version": data.Version,
		})
		return
	}
	// Clients revalidate periodically while the layer is open. Authorization
	// and the current input signature are always checked before any 304.
	tag := `"` + data.Version + `"`
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, data)
}
