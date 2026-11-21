package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

const (
	viewportURL        = "https://tailwindkompass.com/explore"
	viewportLimit      = 8 << 20
	endpointToleranceM = 10
)

type viewportRow struct {
	ID       json.Number `json:"id"`
	Name     string      `json:"name"`
	Distance float64     `json:"distance"`
	Polyline string      `json:"polyline"`
	StartLat *float64    `json:"start_latitude"`
	StartLon *float64    `json:"start_longitude"`
	EndLat   *float64    `json:"end_latitude"`
	EndLon   *float64    `json:"end_longitude"`
}

func runViewport(activity, responsePath string, output io.Writer) error {
	samples, err := readActivity(activity)
	if err != nil {
		return err
	}
	file, err := os.Open(responsePath)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, viewportLimit+1))
	if err != nil {
		return err
	}
	if len(data) > viewportLimit {
		return errors.New("viewport file exceeds 8 MiB")
	}
	segments, rows, err := parseViewport(data)
	if err != nil {
		return fmt.Errorf("map viewport: %w", err)
	}
	report := struct {
		CandidateSource string      `json:"candidate_source"`
		ActivitySamples int         `json:"activity_samples"`
		ReturnedRows    int         `json:"returned_rows"`
		UsableSegments  int         `json:"usable_segments"`
		Candidates      []candidate `json:"candidates"`
	}{
		CandidateSource: "tailwind-kompass-viewport-capture",
		ActivitySamples: len(samples), ReturnedRows: rows, UsableSegments: len(segments),
		Candidates: discover(samples, segments),
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func parseViewport(data []byte) ([]definition, int, error) {
	var rows []viewportRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, 0, err
	}
	if rows == nil {
		return nil, 0, errors.New("expected a map response array")
	}
	out := make([]definition, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		id := row.ID.String()
		if seen[id] || !segmentIDPattern.MatchString(id) ||
			strings.TrimSpace(row.Name) == "" || row.Distance <= 0 {
			continue
		}
		points, err := domain.DecodePolyline(row.Polyline, 5)
		if err != nil || len(points) < 2 || !row.hasEndpoints(points) {
			continue
		}
		valid := true
		for _, point := range points {
			valid = valid && validPoint(point)
		}
		if !valid {
			continue
		}
		out = append(out, definition{
			ID: id, Name: row.Name, ViewportDistanceM: row.Distance,
			Geometry: points, SourceURL: viewportURL,
		})
		seen[id] = true
	}
	if len(rows) > 0 && len(out) == 0 {
		return nil, len(rows), errors.New("no rows had identifiable segments and geometry with matching endpoints")
	}
	return out, len(rows), nil
}

func (r viewportRow) hasEndpoints(points []domain.GeoPoint) bool {
	if r.StartLat == nil || r.StartLon == nil || r.EndLat == nil || r.EndLon == nil {
		return false
	}
	start := domain.GeoPoint{Lat: *r.StartLat, Lon: *r.StartLon}
	end := domain.GeoPoint{Lat: *r.EndLat, Lon: *r.EndLon}
	return validPoint(start) && validPoint(end) &&
		domain.HaversineMeters(start, points[0]) <= endpointToleranceM &&
		domain.HaversineMeters(end, points[len(points)-1]) <= endpointToleranceM
}
