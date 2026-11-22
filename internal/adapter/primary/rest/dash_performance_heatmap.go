package rest

import (
	"net/http"
	"strconv"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/heatmap"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func (h *DashboardHandlers) performanceHeatmap(w http.ResponseWriter, r *http.Request) {
	ctx, member, ok := h.Auth.requireMember(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	viewport := port.HeatmapViewport{}
	if raw := query.Get("level"); raw != "" {
		level, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "Invalid heatmap level", http.StatusBadRequest)
			return
		}
		viewport.Level = heatmap.Level(level)
	}
	if query.Has("west") || query.Has("south") || query.Has("east") || query.Has("north") {
		bounds := domain.CatalogSegmentBounds{}
		for _, coord := range []struct {
			key    string
			target *float64
		}{{"west", &bounds.MinLon}, {"south", &bounds.MinLat}, {"east", &bounds.MaxLon}, {"north", &bounds.MaxLat}} {
			value, err := strconv.ParseFloat(query.Get(coord.key), 64)
			if err != nil {
				http.Error(w, "Invalid heatmap viewport", http.StatusBadRequest)
				return
			}
			*coord.target = value
		}
		viewport.Bounds = &bounds
	}

	data, err := h.PerformanceMaps.Get(ctx, member, dashActivityFilter(query), viewport)
	if err != nil {
		h.Log.Warn("load performance heatmap", "error", err)
		writeActionError(w, err, "Unable to load the heatmap")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if data.Pending > 0 {
		writeJSON(w, http.StatusAccepted, map[string]int{"matched": data.Matched, "with_track": data.WithTrack, "pending": data.Pending})
		return
	}
	features := make([]performanceFeature, 0, len(data.Bins))
	for _, bin := range data.Bins {
		feature := performanceFeatureOf(bin)
		if heatmapLineVisible(feature.Geometry.Coordinates, viewport.Bounds) {
			features = append(features, feature)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"matched": data.Matched, "with_track": data.WithTrack,
		"facets":  map[string]any{"types": dashFacets(data.Types), "disciplines": dashFacets(data.Disciplines), "years": dashYearFacets(data.Years)},
		"geojson": map[string]any{"type": "FeatureCollection", "features": features},
	})
}

type performanceFeature struct {
	Type     string `json:"type"`
	Geometry struct {
		Type        string        `json:"type"`
		Coordinates [2][2]float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		Density   uint32   `json:"density"`
		Speed     float64  `json:"speed"`
		HeartRate *float64 `json:"heart_rate"`
		Elevation *float64 `json:"elevation"`
		Gradient  *float64 `json:"gradient"`
		Power     *float64 `json:"power"`
	} `json:"properties"`
}

func performanceFeatureOf(bin heatmap.Bin) performanceFeature {
	feature := performanceFeature{Type: "Feature"}
	feature.Geometry.Type = "LineString"
	for i, point := range bin.Line() {
		feature.Geometry.Coordinates[i] = [2]float64{dashRound5(min(180, max(-180, point.Lon))), dashRound5(point.Lat)}
	}
	feature.Properties.Density = bin.Passages
	feature.Properties.Speed = domain.Round1(bin.Distance / bin.Moving * 3.6)
	feature.Properties.HeartRate = roundedMapValue(bin.HeartRate)
	feature.Properties.Power = roundedMapValue(bin.Power)
	feature.Properties.Elevation = roundedMapValue(bin.Elevation)
	feature.Properties.Gradient = roundedMapValue(bin.Gradient)
	return feature
}

func roundedMapValue(stat heatmap.Weighted) *float64 {
	if v := stat.Value(); v != nil {
		return new(domain.Round1(*v))
	}
	return nil
}

func heatmapLineVisible(line [2][2]float64, bounds *domain.CatalogSegmentBounds) bool {
	if bounds == nil {
		return true
	}
	south, north := min(line[0][1], line[1][1]), max(line[0][1], line[1][1])
	if south > bounds.MaxLat || north < bounds.MinLat {
		return false
	}
	west, east := min(line[0][0], line[1][0]), max(line[0][0], line[1][0])
	if bounds.MinLon < bounds.MaxLon {
		return west <= bounds.MaxLon && east >= bounds.MinLon
	}
	return east >= bounds.MinLon || west <= bounds.MaxLon
}
