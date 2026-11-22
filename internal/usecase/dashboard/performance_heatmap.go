package dashboard

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/heatmap"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type PerformanceMaps struct {
	activities port.ActivityReader
	repo       port.PerformanceHeatmapRepo
	building   sync.Mutex
}

func NewPerformanceMaps(activities port.ActivityReader, repo port.PerformanceHeatmapRepo) *PerformanceMaps {
	return &PerformanceMaps{activities: activities, repo: repo}
}

type PerformanceMap struct {
	Matched, WithTrack, Pending int
	Types, Disciplines          []port.ActivityFacet
	Years                       []port.ActivityYearStat
	Bins                        []heatmap.Bin
}

func (m *PerformanceMaps) Get(ctx context.Context, member domain.UserID, filter port.ActivityListFilter, viewport port.HeatmapViewport) (PerformanceMap, error) {
	if member == (domain.UserID{}) {
		return PerformanceMap{}, port.NewActionError(port.ActionErrorForbidden, "Authentication required")
	}
	if viewport.Level < heatmap.Overview || viewport.Level > heatmap.Detail || !heatmapBoundsValid(viewport.Bounds) {
		return PerformanceMap{}, port.NewActionError(port.ActionErrorInvalid, "Invalid heatmap viewport")
	}
	filter.Type = string(domain.ActivityTypeRide)
	rides, err := m.repo.HeatmapActivities(ctx, member, filter)
	if err != nil {
		return PerformanceMap{}, err
	}
	out := PerformanceMap{Matched: len(rides)}
	var incomplete []port.HeatmapActivity
	for _, ride := range rides {
		if ride.WithTrack {
			out.WithTrack++
		}
		if !ride.Ready && ride.Version != "" {
			incomplete = append(incomplete, ride)
		}
	}
	if len(incomplete) > 0 {
		if err := m.prepare(ctx, incomplete); err != nil {
			return PerformanceMap{}, err
		}
		// The client polls until every eligible activity has been indexed.
		// Re-read versions on the next call in case an import won a race.
		out.Pending = len(incomplete)
		return out, nil
	}
	_, out.Disciplines, _, err = m.activities.ActivityFacets(ctx, member, filter)
	if err != nil {
		return out, err
	}
	out.Types = []port.ActivityFacet{{Value: filter.Type, Count: out.Matched}}
	out.Years, err = m.activities.ActivityYearStats(ctx, member)
	if err != nil {
		return out, err
	}

	bins := make(map[heatmap.Key]*heatmap.Bin)
	err = m.repo.VisitHeatmapBins(ctx, member, rides, viewport, func(bin heatmap.Bin) {
		merged := bins[bin.Key]
		if merged == nil {
			merged = &heatmap.Bin{}
			bins[bin.Key] = merged
		}
		merged.Merge(bin)
	})
	if err != nil {
		return out, err
	}
	out.Bins = heatmap.Ordered(bins)
	return out, nil
}

func heatmapBoundsValid(bounds *domain.CatalogSegmentBounds) bool {
	if bounds == nil {
		return true
	}
	return bounds.MinLon >= -180 && bounds.MinLon <= 180 && bounds.MaxLon >= -180 && bounds.MaxLon <= 180 &&
		bounds.MinLat >= -heatmap.MaxLatitude && bounds.MaxLat <= heatmap.MaxLatitude && bounds.MinLat < bounds.MaxLat &&
		bounds.MinLon != bounds.MaxLon && !(bounds.MinLon == 180 && bounds.MaxLon == -180)
}

// Prepare in short, mutually exclusive batches. Cached rides are reused
// across filters, so subsequent reads only touch the relevant map tiles.
func (m *PerformanceMaps) prepare(ctx context.Context, rides []port.HeatmapActivity) error {
	if !m.building.TryLock() {
		return nil
	}
	defer m.building.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for i, ride := range rides[:min(24, len(rides))] {
		if i > 0 && time.Now().After(deadline) {
			break
		}
		primary, err := m.repo.HeatmapSamples(ctx, ride.Primary, true)
		if err != nil {
			return fmt.Errorf("load heatmap track: %w", err)
		}
		if len(primary) > 1 {
			for _, source := range ride.Sources {
				if source == ride.Primary {
					continue
				}
				sensors, err := m.repo.HeatmapSamples(ctx, source, false)
				if err != nil {
					return fmt.Errorf("load heatmap sensors: %w", err)
				}
				fillHeatmapSensors(primary, sensors)
			}
		}
		if err := m.repo.SaveHeatmapBins(ctx, ride, heatmap.Aggregate(primary)); err != nil {
			return fmt.Errorf("cache heatmap track: %w", err)
		}
	}
	return nil
}

// A second source may supply a missing sensor at the same instant. It never
// contributes another route or another weight to an already recorded channel.
func fillHeatmapSensors(primary, extra []domain.StreamSample) {
	if len(extra) == 0 {
		return
	}
	j := 0
	for i := range primary {
		p := &primary[i]
		for j+1 < len(extra) && deltaSeconds(extra[j+1].Timestamp, p.Timestamp) <= deltaSeconds(extra[j].Timestamp, p.Timestamp) {
			j++
		}
		match := extra[j]
		if deltaSeconds(match.Timestamp, p.Timestamp) > 1 {
			continue
		}
		if p.HeartRateBpm == nil && match.HeartRateBpm != nil && *match.HeartRateBpm >= 25 && *match.HeartRateBpm <= 250 {
			p.HeartRateBpm = match.HeartRateBpm
		}
		if p.PowerW == nil && match.PowerW != nil && *match.PowerW >= 0 && *match.PowerW <= 3000 {
			p.PowerW = match.PowerW
		}
	}
}

func deltaSeconds(a, b time.Time) float64 { return math.Abs(a.Sub(b).Seconds()) }
