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
	"github.com/opencairn-org/cairn-core/internal/domain/heatmap"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

type heatmapActivityReader interface {
	port.ActivityReader
	port.SourceReader
}

type PerformanceMaps struct {
	activities heatmapActivityReader
	repo       port.PerformanceHeatmapRepo
	building   sync.Mutex
	Visibility *view.Visibility
}

func NewPerformanceMaps(activities heatmapActivityReader, repo port.PerformanceHeatmapRepo) *PerformanceMaps {
	return &PerformanceMaps{activities: activities, repo: repo}
}

type PerformanceMap struct {
	Matched, WithTrack, Pending int
	Types, Disciplines          []port.ActivityFacet
	Years                       []port.ActivityYearStat
	Bins                        []heatmap.Bin
}

// Get merges the same visible per-rider bins for personal and community maps.
func (m *PerformanceMaps) Get(ctx context.Context, viewer domain.UserID, members []domain.UserID, filter port.ActivityListFilter, viewport port.HeatmapViewport) (PerformanceMap, error) {
	if viewer == (domain.UserID{}) {
		return PerformanceMap{}, port.NewActionError(port.ActionErrorForbidden, "Authentication required")
	}
	if viewport.Level < heatmap.Overview || viewport.Level > heatmap.Detail || !heatmapBoundsValid(viewport.Bounds) {
		return PerformanceMap{}, port.NewActionError(port.ActionErrorInvalid, "Invalid heatmap viewport")
	}
	filter.Type = string(domain.ActivityTypeRide)
	rides := make([][]port.HeatmapActivity, len(members))
	readRides := func() (PerformanceMap, []port.HeatmapActivity, error) {
		var out PerformanceMap
		var incomplete []port.HeatmapActivity
		for i, member := range members {
			read := port.WithActivityReadViewer(ctx, viewer == member)
			var err error
			rides[i], err = m.repo.HeatmapActivities(read, member, filter)
			if err != nil {
				return out, nil, err
			}
			out.Matched += len(rides[i])
			for _, ride := range rides[i] {
				if ride.WithTrack {
					out.WithTrack++
				}
				if !ride.Ready && ride.Version != "" {
					incomplete = append(incomplete, ride)
				}
			}
		}
		out.Pending = len(incomplete)
		return out, incomplete, nil
	}
	out, incomplete, err := readRides()
	if err != nil {
		return out, err
	}
	if len(incomplete) > 0 {
		prepared, err := m.prepare(ctx, incomplete)
		if err != nil {
			return out, err
		}
		if prepared {
			// Re-read persisted readiness and any source versions changed by an import.
			out, _, err = readRides()
		}
		if err != nil || out.Pending > 0 {
			return out, err
		}
	}

	bins := make(map[heatmap.Key]*heatmap.Bin)
	disciplines := make(map[string]int)
	years := make(map[int]port.ActivityYearStat)
	for i, member := range members {
		read := port.WithActivityReadViewer(ctx, viewer == member)
		_, memberDisciplines, _, err := m.activities.ActivityFacets(read, member, filter)
		if err != nil {
			return out, err
		}
		for _, facet := range memberDisciplines {
			disciplines[facet.Value] += facet.Count
		}
		memberYears, err := m.activities.ActivityYearStats(read, member)
		if err != nil {
			return out, err
		}
		for _, year := range memberYears {
			current := years[year.Year]
			current.Year, current.Count = year.Year, current.Count+year.Count
			years[year.Year] = current
		}
		err = m.repo.VisitHeatmapBins(read, member, rides[i], viewport, func(bin heatmap.Bin) {
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
	}
	for value, count := range disciplines {
		out.Disciplines = append(out.Disciplines, port.ActivityFacet{Value: value, Count: count})
	}
	slices.SortFunc(out.Disciplines, func(a, b port.ActivityFacet) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Value, b.Value))
	})
	for _, year := range years {
		out.Years = append(out.Years, year)
	}
	slices.SortFunc(out.Years, func(a, b port.ActivityYearStat) int { return cmp.Compare(b.Year, a.Year) })
	out.Types = []port.ActivityFacet{{Value: filter.Type, Count: out.Matched}}
	out.Bins = heatmap.Ordered(bins)
	return out, nil
}

type HeatmapRiderPassages struct {
	Rider      domain.UserID
	Passages   uint64
	Activities int
}

type HeatmapDiscovery struct {
	Riders     []HeatmapRiderPassages
	Passages   uint64
	Matched    int
	Activities []domain.Activity
}

func (m *PerformanceMaps) Discover(ctx context.Context, viewer domain.UserID, members []domain.UserID, selected domain.UserID, filter port.ActivityListFilter, cell port.HeatmapCell) (HeatmapDiscovery, error) {
	var out HeatmapDiscovery
	if viewer == (domain.UserID{}) {
		return out, port.NewActionError(port.ActionErrorForbidden, "Authentication required")
	}
	if !cell.Key.Valid(cell.Level) {
		return out, port.NewActionError(port.ActionErrorInvalid, "Invalid heatmap cell")
	}
	filter.Type = string(domain.ActivityTypeRide)
	var ids []domain.ActivityID
	for _, member := range members {
		read := port.WithActivityReadViewer(ctx, viewer == member)
		contributions, err := m.repo.HeatmapContributors(read, member, filter, cell)
		if err != nil {
			return out, err
		}
		if len(contributions) == 0 {
			continue
		}
		rider := HeatmapRiderPassages{Rider: member, Activities: len(contributions)}
		for _, item := range contributions {
			rider.Passages += uint64(item.Passages)
			if selected == (domain.UserID{}) || selected == member {
				ids = append(ids, item.Activity)
			}
		}
		out.Riders = append(out.Riders, rider)
		out.Passages += rider.Passages
	}
	slices.SortStableFunc(out.Riders, func(a, b HeatmapRiderPassages) int {
		return cmp.Compare(b.Passages, a.Passages)
	})
	out.Matched = len(ids)
	var err error
	out.Activities, err = m.repo.HeatmapResultActivities(ctx, viewer, ids, filter.Sort, filter.Limit, filter.Offset)
	return out, err
}

// Track returns the complete visible primary GPS track for a selected ride.
func (m *PerformanceMaps) Track(ctx context.Context, viewer domain.UserID, id domain.ActivityID) (domain.RecordedTrack, error) {
	activity, err := m.activities.GetActivity(ctx, id)
	if err != nil {
		return nil, err
	}
	access, visible := m.Visibility.Resolve(ctx, activity, &viewer)
	if !visible || activity.Type != domain.ActivityTypeRide || activity.PrimaryStreamSourceID == nil {
		return nil, domain.ErrNotFound
	}
	source, err := m.activities.GetSource(ctx, *activity.PrimaryStreamSourceID)
	if err != nil {
		return nil, err
	}
	if source.ActivityID != id || source.Status == domain.SourceStatusDetached || source.Provider == "derived" ||
		(domain.ProviderDataRestricted(source.Provider) && !(access.IsOwner && activity.ProviderRestricted)) {
		return nil, domain.ErrNotFound
	}
	samples, err := m.repo.HeatmapSamples(ctx, source.ID, true)
	if err != nil {
		return nil, err
	}
	track, _ := domain.CyclingGPSTrack(samples)
	// Check privacy again after simplification, which can cut across a zone
	// even when every leg of the recorded track was outside it.
	return track.Mask(access.Zones).MapGeometry().Mask(access.Zones), nil
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
// The boolean indicates whether this request acquired the preparation lock.
func (m *PerformanceMaps) prepare(ctx context.Context, rides []port.HeatmapActivity) (bool, error) {
	if !m.building.TryLock() {
		return false, nil
	}
	defer m.building.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for i, ride := range rides[:min(24, len(rides))] {
		if i > 0 && time.Now().After(deadline) {
			break
		}
		primary, err := m.repo.HeatmapSamples(ctx, ride.Primary, true)
		if err != nil {
			return true, fmt.Errorf("load heatmap track: %w", err)
		}
		if len(primary) > 1 {
			for _, source := range ride.Sources {
				if source == ride.Primary {
					continue
				}
				sensors, err := m.repo.HeatmapSamples(ctx, source, false)
				if err != nil {
					return true, fmt.Errorf("load heatmap sensors: %w", err)
				}
				fillHeatmapSensors(primary, sensors)
			}
		}
		if err := m.repo.SaveHeatmapBins(ctx, ride, heatmap.Aggregate(primary)); err != nil {
			return true, fmt.Errorf("cache heatmap track: %w", err)
		}
	}
	return true, nil
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
		if p.Cadence == nil && match.Cadence != nil && *match.Cadence >= 0 && *match.Cadence <= 255 {
			p.Cadence = match.Cadence
		}
	}
}

func deltaSeconds(a, b time.Time) float64 { return math.Abs(a.Sub(b).Seconds()) }
