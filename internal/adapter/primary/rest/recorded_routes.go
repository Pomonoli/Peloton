package rest

import (
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/export"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/routes"
)

type RecordedRouteHandlers struct {
	Auth    *Auth
	Catalog *routes.Catalog
	Log     *slog.Logger
}

func (h *RecordedRouteHandlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/routes", h.list)
	mux.HandleFunc("GET /api/routes/map", h.routeMap)
	mux.HandleFunc("GET /api/routes/{id}", h.detail)
	mux.HandleFunc("GET /api/routes/{id}/activities", h.activities)
	mux.HandleFunc("GET /api/routes/{id}/gpx", h.gpx)
}

func (h *RecordedRouteHandlers) caller(w http.ResponseWriter, r *http.Request) (domain.UserID, bool) {
	caller, ok := h.Auth.Callers.Authenticate(r.Context(), credentialOf(r))
	switch {
	case !ok:
		http.Error(w, "Unauthenticated", http.StatusUnauthorized)
	case caller.Scopes != nil && !slices.Contains(caller.Scopes, domain.ScopeActivitiesRead):
		http.Error(w, "Activity read permission required", http.StatusForbidden)
	default:
		w.Header().Set("Cache-Control", "private, no-store")
		return caller.UserID, true
	}
	return domain.UserID{}, false
}

func recordedRouteFilters(q url.Values) (port.RecordedRouteQuery, string) {
	kind := domain.RecordedRouteKind(q.Get("type"))
	switch kind {
	case "", domain.RecordedLoop, domain.RecordedOutAndBack, domain.RecordedPointToPoint:
	default:
		return port.RecordedRouteQuery{}, "Invalid route type"
	}
	minimum, maximum := recordedDistance(q.Get("min_km")), recordedDistance(q.Get("max_km"))
	if minimum < 0 || maximum < 0 || (maximum > 0 && minimum >= maximum) {
		return port.RecordedRouteQuery{}, "Invalid route distance"
	}
	return port.RecordedRouteQuery{
		Rider: strings.TrimSpace(q.Get("rider")), Search: strings.TrimSpace(q.Get("q")), Sort: q.Get("sort"),
		Kind: kind, MinDistanceM: minimum, MaxDistanceM: maximum, Unridden: q.Get("unridden") == "true",
	}, ""
}

func (h *RecordedRouteHandlers) list(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.caller(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	filter, invalid := recordedRouteFilters(q)
	if invalid != "" {
		http.Error(w, invalid, http.StatusBadRequest)
		return
	}
	filter.Offset = recordedPage(q.Get("offset"), 0, 1_000_000)
	filter.Limit = recordedPage(q.Get("limit"), 12, 50)
	out, err := h.Catalog.List(r.Context(), viewer, filter)
	if err != nil {
		h.failed(w, err)
		return
	}
	items := make([]map[string]any, 0, len(out.Routes))
	for _, route := range out.Routes {
		items = append(items, recordedSummaryJSON(route))
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": items, "total": out.Total})
}

type recordedMapFeature struct {
	Type       string                `json:"type"`
	Geometry   recordedMapGeometry   `json:"geometry"`
	Properties recordedMapProperties `json:"properties"`
}

type recordedMapGeometry struct {
	Type        string       `json:"type"`
	Coordinates [][2]float64 `json:"coordinates"`
}

type recordedMapProperties struct {
	Routes  []int `json:"routes"`
	Density int   `json:"density"`
}

func (h *RecordedRouteHandlers) routeMap(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.caller(w, r)
	if !ok {
		return
	}
	filter, invalid := recordedRouteFilters(r.URL.Query())
	if invalid != "" {
		http.Error(w, invalid, http.StatusBadRequest)
		return
	}
	filter.Map = true
	out, err := h.Catalog.List(r.Context(), viewer, filter)
	if err != nil {
		h.failed(w, err)
		return
	}
	tracks := make([]domain.RecordedTrack, 0, len(out.Routes))
	routes := make([]map[string]any, 0, len(out.Routes))
	for _, route := range out.Routes {
		tracks = append(tracks, route.Track)
		routes = append(routes, recordedSummaryJSON(route))
	}
	lines, err := domain.RecordedRouteMap(r.Context(), tracks)
	if err != nil {
		return
	}
	features := make([]recordedMapFeature, 0, len(lines))
	for _, line := range lines {
		coords := make([][2]float64, 0, len(line.Points))
		origin := line.Points[0].Lon
		for _, point := range line.Points {
			coords = append(coords, [2]float64{origin + math.Remainder(point.Lon-origin, 360), point.Lat})
		}
		features = append(features, recordedMapFeature{
			Type:       "Feature",
			Geometry:   recordedMapGeometry{Type: "LineString", Coordinates: coords},
			Properties: recordedMapProperties{Routes: line.Routes, Density: len(line.Routes)},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"routes":  routes,
		"geojson": map[string]any{"type": "FeatureCollection", "features": features},
	})
}

func (h *RecordedRouteHandlers) get(w http.ResponseWriter, r *http.Request, offset, limit int) (port.RecordedRouteDetail, bool) {
	viewer, ok := h.caller(w, r)
	if !ok {
		return port.RecordedRouteDetail{}, false
	}
	id, err := domain.ParseUUID[domain.RecordedRouteID](r.PathValue("id"))
	if err != nil || id == (domain.RecordedRouteID{}) {
		http.Error(w, "Invalid route ID", http.StatusBadRequest)
		return port.RecordedRouteDetail{}, false
	}
	out, err := h.Catalog.Detail(r.Context(), viewer, id, offset, limit)
	if err != nil {
		h.failed(w, err)
		return port.RecordedRouteDetail{}, false
	}
	return out, true
}

func (h *RecordedRouteHandlers) detail(w http.ResponseWriter, r *http.Request) {
	out, ok := h.get(w, r, 0, 5)
	if !ok {
		return
	}
	item := recordedSummaryJSON(out.RecordedRouteSummary)
	item["recent"] = recordedRidesJSON(out.Recent)
	writeJSON(w, http.StatusOK, item)
}

func (h *RecordedRouteHandlers) activities(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, ok := h.get(w, r, recordedPage(q.Get("offset"), 0, 1_000_000), 20)
	if ok {
		writeJSON(w, http.StatusOK, map[string]any{"recent": recordedRidesJSON(out.Recent), "total": out.Rides})
	}
}

func (h *RecordedRouteHandlers) gpx(w http.ResponseWriter, r *http.Request) {
	out, ok := h.get(w, r, 0, 0)
	if !ok {
		return
	}
	if len(out.Track) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/gpx+xml")
	w.Header().Set("Content-Disposition", `attachment; filename="peloton-route-`+out.ID.String()+`.gpx"`)
	_, _ = w.Write(export.RecordedRouteGPX(out.Name, out.Track))
}

func (h *RecordedRouteHandlers) failed(w http.ResponseWriter, err error) {
	h.Log.Debug("load recorded routes", "error", err)
	writeActionError(w, err, "Unable to load routes")
}

func recordedPage(raw string, fallback, maximum int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || (fallback > 0 && value == 0) {
		return fallback
	}
	return min(value, maximum)
}

func recordedDistance(raw string) float64 {
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100_000 {
		return -1
	}
	return value * 1000
}

type recordedRoutePointJSON struct {
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	DistanceM  float64  `json:"distance_m"`
	ElevationM *float64 `json:"elevation_m,omitempty"`
}

func recordedSummaryJSON(route port.RecordedRouteSummary) map[string]any {
	track := make([][]recordedRoutePointJSON, len(route.Track))
	for i, line := range route.Track {
		track[i] = make([]recordedRoutePointJSON, len(line))
		for j, p := range line {
			track[i][j] = recordedRoutePointJSON{Lat: p.Lat, Lon: p.Lon, DistanceM: p.DistanceM, ElevationM: p.ElevationM}
		}
	}
	riders := make([]map[string]any, 0, len(route.Riders))
	for _, rider := range route.Riders {
		riders = append(riders, map[string]any{
			"id": rider.UserID.String(), "username": rider.Username, "display_name": rider.DisplayName, "avatar_url": rider.AvatarURL,
			"rides": rider.Rides, "best_speed_kmh": rider.BestSpeedKmh,
		})
	}
	return map[string]any{
		"id": route.ID.String(), "name": route.Name, "type": route.Kind, "distance_m": route.DistanceM,
		"elevation_gain_m": route.ElevationM, "rides": route.Rides, "rider_count": route.RiderCount,
		"best_speed_kmh": route.BestSpeedKmh, "track": track, "riders": riders,
	}
}

func recordedRidesJSON(activities []port.RecordedRouteRide) []map[string]any {
	out := make([]map[string]any, 0, len(activities))
	for _, ride := range activities {
		out = append(out, map[string]any{
			"id": ride.ID.String(), "rider_id": ride.Rider.String(), "title": ride.Title,
			"start_time": ride.StartTime.UTC(), "timezone": ride.Timezone, "distance_m": ride.DistanceM, "speed_kmh": ride.SpeedKmh,
		})
	}
	return out
}
