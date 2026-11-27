package rest

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/heatmap"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func (h *DashboardHandlers) performanceHeatmap(w http.ResponseWriter, r *http.Request) {
	ctx, viewer, members, ok := h.heatmapMembers(w, r)
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

	data, err := h.PerformanceMaps.Get(ctx, viewer, heatmapMemberIDs(members), dashActivityFilter(query), viewport)
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
		feature := performanceFeatureOf(bin, viewport.Level)
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

func (h *DashboardHandlers) heatmapMembers(w http.ResponseWriter, r *http.Request) (context.Context, domain.UserID, []domain.CommunityMember, bool) {
	viewer, ok := h.Auth.requireUser(w, r)
	ctx := port.WithCommunityViewer(r.Context(), viewer)
	if !ok {
		return ctx, viewer, nil, false
	}
	if r.URL.Query().Get("riders") == "all" {
		members, err := h.Auth.Members.List(ctx, viewer)
		if err != nil {
			writeActionError(w, err, "Unable to load riders")
			return ctx, viewer, nil, false
		}
		return ctx, viewer, members, true
	}
	member := viewer
	if key := r.URL.Query().Get("member"); key != "" {
		rider, err := h.Auth.Members.Get(ctx, viewer, key)
		if err != nil {
			writeActionError(w, err, "Unable to load the member")
			return ctx, viewer, nil, false
		}
		member = rider.ID
		return ctx, viewer, []domain.CommunityMember{{
			UserID: member, Username: rider.Username, DisplayName: rider.DisplayName,
		}}, true
	}
	return ctx, viewer, []domain.CommunityMember{{UserID: member}}, true
}

func heatmapMemberIDs(members []domain.CommunityMember) []domain.UserID {
	ids := make([]domain.UserID, len(members))
	for i, member := range members {
		ids[i] = member.UserID
	}
	return ids
}

func parseHeatmapCell(raw string) (port.HeatmapCell, bool) {
	parts := strings.Split(raw, ":")
	if len(parts) != 4 {
		return port.HeatmapCell{}, false
	}
	var values [4]int64
	for i, part := range parts {
		value, err := strconv.ParseInt(part, 10, 32)
		if err != nil {
			return port.HeatmapCell{}, false
		}
		values[i] = value
	}
	if values[1] < 0 || values[1] > 3 {
		return port.HeatmapCell{}, false
	}
	cell := port.HeatmapCell{Level: heatmap.Level(values[0]), Key: heatmap.Key{
		Axis: uint8(values[1]), Along: int32(values[2]), Across: int32(values[3]),
	}}
	return cell, cell.Key.Valid(cell.Level)
}

type heatmapRiderJSON struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Passages    uint64 `json:"passages"`
	Activities  int    `json:"activities"`
}

type heatmapActivityJSON struct {
	dashFeedActivity
	RiderID string `json:"rider_id"`
}

func (h *DashboardHandlers) discoverHeatmap(w http.ResponseWriter, r *http.Request) {
	ctx, viewer, members, ok := h.heatmapMembers(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	cell, ok := parseHeatmapCell(q.Get("cell"))
	if !ok {
		http.Error(w, "Invalid heatmap cell", http.StatusBadRequest)
		return
	}
	var selected domain.UserID
	if raw := q.Get("rider"); raw != "" {
		id, err := domain.ParseUUID[domain.UserID](raw)
		if err != nil || id == (domain.UserID{}) {
			http.Error(w, "Invalid rider", http.StatusBadRequest)
			return
		}
		selected = id
	}
	filter := dashActivityFilter(q)
	filter.Limit, filter.Offset = dashFeedPaging(q)
	data, err := h.PerformanceMaps.Discover(ctx, viewer, heatmapMemberIDs(members), selected, filter, cell)
	if err != nil {
		h.Log.Warn("discover heatmap activities", "error", err)
		writeActionError(w, err, "Unable to load matching activities")
		return
	}
	memberByID := make(map[domain.UserID]domain.CommunityMember, len(members))
	for _, member := range members {
		memberByID[member.UserID] = member
	}
	riders := make([]heatmapRiderJSON, 0, len(data.Riders))
	for _, rider := range data.Riders {
		member := memberByID[rider.Rider]
		riders = append(riders, heatmapRiderJSON{
			ID: rider.Rider.String(), Username: member.Username, DisplayName: member.DisplayName,
			Passages: rider.Passages, Activities: rider.Activities,
		})
	}
	activities := make([]heatmapActivityJSON, 0, len(data.Activities))
	for _, activity := range data.Activities {
		read := port.WithActivityReadViewer(ctx, viewer == activity.UserID)
		activities = append(activities, heatmapActivityJSON{
			dashFeedActivity: dashFeedActivityOf(h.redactActivity(read, activity), false),
			RiderID:          activity.UserID.String(),
		})
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"passages": data.Passages, "riders": riders, "matched": data.Matched,
		"activities": activities, "has_more": filter.Offset+len(activities) < data.Matched,
	})
}

func (h *DashboardHandlers) heatmapTrack(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.Auth.requireUser(w, r)
	if !ok {
		return
	}
	id, err := domain.ParseUUID[domain.ActivityID](r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid activity ID", http.StatusBadRequest)
		return
	}
	track, err := h.PerformanceMaps.Track(r.Context(), viewer, id)
	if err != nil {
		writeActionError(w, err, "Unable to load the activity track")
		return
	}
	lines := make([][][2]float64, 0, len(track))
	for _, line := range track {
		points := make([][2]float64, 0, len(line))
		for _, p := range line {
			points = append(points, [2]float64{p.Lon, p.Lat})
		}
		lines = append(lines, points)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

type performanceFeature struct {
	Type     string `json:"type"`
	Geometry struct {
		Type        string        `json:"type"`
		Coordinates [2][2]float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		Cell      string   `json:"cell"`
		Density   uint32   `json:"density"`
		Speed     float64  `json:"speed"`
		HeartRate *float64 `json:"heart_rate"`
		Elevation *float64 `json:"elevation"`
		Gradient  *float64 `json:"gradient"`
		Power     *float64 `json:"power"`
	} `json:"properties"`
}

func performanceFeatureOf(bin heatmap.Bin, level heatmap.Level) performanceFeature {
	feature := performanceFeature{Type: "Feature"}
	feature.Geometry.Type = "LineString"
	for i, point := range bin.Line() {
		feature.Geometry.Coordinates[i] = [2]float64{dashRound5(min(180, max(-180, point.Lon))), dashRound5(point.Lat)}
	}
	feature.Properties.Cell = fmt.Sprintf("%d:%d:%d:%d", level, bin.Key.Axis, bin.Key.Along, bin.Key.Across)
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
