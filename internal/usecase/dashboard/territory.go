package dashboard

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/territory"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type TerritoryMaps struct {
	repo    port.TerritoryRepo
	samples interface {
		HeatmapSamples(context.Context, domain.SourceID, bool) ([]domain.StreamSample, error)
	}
	mu       sync.Mutex
	snapshot *TerritoryMap
}

func NewTerritoryMaps(repo port.TerritoryRepo, samples port.PerformanceHeatmapRepo) *TerritoryMaps {
	return &TerritoryMaps{repo: repo, samples: samples}
}

type TerritoryLegendEntry struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Color      string  `json:"color"`
	Cells      int     `json:"cells"`
	AreaKm2    float64 `json:"area_km2"`
	Percentage float64 `json:"percentage"`
}

type TerritoryDistance struct {
	RiderID   string  `json:"rider_id"`
	DistanceM float64 `json:"distance_m"`
}

type TerritoryGeometry struct {
	Type        string           `json:"type"`
	Coordinates [][][][2]float64 `json:"coordinates"`
}

type TerritoryFeature struct {
	Type       string            `json:"type"`
	Geometry   TerritoryGeometry `json:"geometry"`
	Properties map[string]string `json:"properties"`
}

type TerritoryFeatureCollection struct {
	Type     string             `json:"type"`
	Features []TerritoryFeature `json:"features"`
}

type TerritoryMap struct {
	Version string                     `json:"version"`
	Pending int                        `json:"pending,omitempty"`
	GeoJSON TerritoryFeatureCollection `json:"geojson"`
	Legend  []TerritoryLegendEntry     `json:"legend"`
	cells   map[territory.Cell][]TerritoryDistance
}

// At identifies the underlying hexagon independently of its dissolved map
// polygon. The first contribution, when present, is its owner.
func (m *TerritoryMap) At(lat, lon float64) ([]TerritoryDistance, error) {
	cell, err := territory.CellAt(lat, lon)
	if err != nil {
		return nil, err
	}
	return m.cells[cell], nil
}

// Get advances the bounded backfill on demand, using the same raw streams
// as the other heatmap layers. An eligible ride is parsed again only when
// its primary source or stream revision changes.
func (m *TerritoryMaps) Get(ctx context.Context, viewer domain.UserID, now time.Time) (*TerritoryMap, error) {
	if viewer == (domain.UserID{}) {
		return nil, port.NewActionError(port.ActionErrorForbidden, "Authentication required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var version string
	if m.snapshot != nil {
		version = m.snapshot.Version
	}
	input, err := m.repo.ReadTerritory(ctx, now, version)
	if err != nil {
		return nil, err
	}
	if input.Pending > 0 {
		if err := m.prepare(ctx, input.Rides); err != nil {
			return nil, err
		}
		return &TerritoryMap{Pending: input.Pending}, nil
	}
	if input.Version == version && m.snapshot != nil {
		return m.snapshot, nil
	}
	snapshot, err := buildTerritoryMap(input)
	if err != nil {
		return nil, err
	}
	m.snapshot = snapshot
	return snapshot, nil
}

func (m *TerritoryMaps) prepare(ctx context.Context, rides []port.TerritoryRide) error {
	deadline := time.Now().Add(2 * time.Second)
	for i, ride := range rides {
		if i > 0 && time.Now().After(deadline) {
			break
		}
		samples, err := m.samples.HeatmapSamples(ctx, ride.Source, true)
		if err != nil {
			return fmt.Errorf("read territory track: %w", err)
		}
		distances, err := territory.Distances(samples)
		if err != nil {
			return fmt.Errorf("measure territory track: %w", err)
		}
		if err := m.repo.SaveTerritory(ctx, ride, distances); err != nil {
			return fmt.Errorf("cache territory distances: %w", err)
		}
	}
	return nil
}

func buildTerritoryMap(input port.TerritoryRead) (*TerritoryMap, error) {
	out := &TerritoryMap{
		Version: input.Version,
		GeoJSON: TerritoryFeatureCollection{Type: "FeatureCollection", Features: []TerritoryFeature{}},
		Legend:  make([]TerritoryLegendEntry, len(input.Riders)),
		cells:   make(map[territory.Cell][]TerritoryDistance),
	}
	index := make(map[domain.UserID]int, len(input.Riders))
	owned := make([][]territory.Cell, len(input.Riders))
	for i, rider := range input.Riders {
		index[rider.UserID] = i
		name := rider.DisplayName
		if name == "" {
			name = rider.Username
		}
		// The golden angle spreads successive account colors around the wheel.
		hue := math.Mod(float64(rider.ColorIndex)*137.50776405+210, 360)
		out.Legend[i] = TerritoryLegendEntry{ID: rider.UserID.String(), Name: name,
			Color: fmt.Sprintf("hsl(%.5f, 72%%, 50%%)", hue)}
	}
	claimedArea, err := out.rank(input, index, owned)
	if err != nil {
		return nil, err
	}
	for i := range out.Legend {
		if len(owned[i]) == 0 {
			continue
		}
		rider := &out.Legend[i]
		rider.Percentage = rider.AreaKm2 / claimedArea * 100
		rings, err := territory.Polygons(owned[i])
		if err != nil {
			return nil, fmt.Errorf("dissolve territory cells: %w", err)
		}
		out.GeoJSON.Features = append(out.GeoJSON.Features, TerritoryFeature{
			Type: "Feature", Geometry: TerritoryGeometry{Type: "MultiPolygon", Coordinates: rings},
			Properties: map[string]string{"rider_id": rider.ID, "color": rider.Color},
		})
	}
	slices.SortStableFunc(out.Legend, func(a, b TerritoryLegendEntry) int {
		return cmp.Or(cmp.Compare(b.AreaKm2, a.AreaKm2), cmp.Compare(a.Name, b.Name))
	})
	return out, nil
}

// The repository orders scores by cell, highest distance, then rider ID.
// Removing masked scores preserves that order for the remaining riders.
func (out *TerritoryMap) rank(input port.TerritoryRead, index map[domain.UserID]int, owned [][]territory.Cell) (float64, error) {
	var claimedArea float64
	for _, score := range input.Scores {
		i := index[score.Rider]
		masked, err := territory.Masked(score.Cell, input.Riders[i].Zones)
		if err != nil {
			return 0, fmt.Errorf("mask territory cell: %w", err)
		}
		if masked {
			continue
		}
		if len(out.cells[score.Cell]) == 0 {
			area, err := score.Cell.AreaKm2()
			if err != nil {
				return 0, err
			}
			owned[i] = append(owned[i], score.Cell)
			out.Legend[i].Cells++
			out.Legend[i].AreaKm2 += area
			claimedArea += area
		}
		out.cells[score.Cell] = append(out.cells[score.Cell], TerritoryDistance{
			RiderID: out.Legend[i].ID, DistanceM: float64(score.DistanceMM) / 1000,
		})
	}
	return claimedArea, nil
}
