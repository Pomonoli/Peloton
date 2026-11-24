// Package dashboard holds member dashboard reads: overview, stats,
// heatmap, search and health series.
package dashboard

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/trainingload"
)

// ErrUnavailable: the read needs a subsystem this instance has not wired.
var ErrUnavailable = port.NewActionError(port.ActionErrorUnavailable, "not available on this instance")

// Stats are the aggregate reads of the stats, analysis, best-effort and
// heatmap pages (StatsService / GET /api/stats, /api/analysis, /api/records,
// /api/best-efforts/history, /api/activities/heatmap).
type Stats struct {
	activities   port.ActivityReader
	metrics      port.MetricRepo
	bestEfforts  port.BestEffortRepo
	streams      port.StreamRepo
	now          func() time.Time
	PrivacyZones port.VisibilityRepo
}

// NewStats wires the repos; metrics, bestEfforts and streams may be nil.
func NewStats(activities port.ActivityReader, metrics port.MetricRepo, bestEfforts port.BestEffortRepo, streams port.StreamRepo, now func() time.Time) *Stats {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Stats{activities: activities, metrics: metrics, bestEfforts: bestEfforts, streams: streams, now: now}
}

// StatsResult is the stats page.
type StatsResult struct {
	Totals          port.ActivityTotalsResult
	BySport         []port.ActivityFacet
	SportTotals     []port.ActivitySportTotals
	ByDiscipline    []port.ActivityFacet
	ByYear          []port.ActivityYearStat
	LongestDistance *domain.Activity
	LongestDuration *domain.Activity
}

// Get computes the stats page.
func (s *Stats) Get(ctx context.Context, userID domain.UserID) (StatsResult, error) {
	totals, err := s.activities.ActivityTotals(ctx, userID)
	if err != nil {
		return StatsResult{}, fmt.Errorf("activity totals: %w", err)
	}
	out := StatsResult{Totals: totals}
	out.BySport, out.ByDiscipline, _, _ = s.activities.ActivityFacets(ctx, userID, port.ActivityListFilter{})
	out.SportTotals, err = s.activities.ActivitySportTotals(ctx, userID)
	if err != nil {
		return StatsResult{}, fmt.Errorf("totals by sport: %w", err)
	}
	out.ByYear, _ = s.activities.ActivityYearStats(ctx, userID)
	out.LongestDistance = s.first(ctx, userID, "distance_desc")
	out.LongestDuration = s.first(ctx, userID, "moving_duration_desc")
	if out.LongestDuration != nil && out.LongestDuration.MovingDuration <= 0 {
		out.LongestDuration = nil
	}
	return out, nil
}

func (s *Stats) first(ctx context.Context, userID domain.UserID, sort string) *domain.Activity {
	acts, _, err := s.activities.ListActivitiesFiltered(ctx, userID, port.ActivityListFilter{Sort: sort, Limit: 1})
	if err != nil || len(acts) == 0 {
		return nil
	}
	return &acts[0]
}

// Training-load window bounds.
const (
	TrainingLoadDefaultDays = 180
	TrainingLoadMaxDays     = 3650
)

// TrainingLoad returns the daily CTL/ATL/TSB/TSS series of the last days.
func (s *Stats) TrainingLoad(ctx context.Context, userID domain.UserID, days int) ([]domain.TrainingLoadDay, error) {
	if s.metrics == nil {
		return nil, ErrUnavailable
	}
	start, end := s.trainingLoadBounds(days)
	if port.IsCommunityActivityRead(ctx) {
		hidden, err := s.activities.HasHiddenTrainingStress(ctx, userID, end)
		if err != nil {
			return nil, fmt.Errorf("check for moderated training activities: %w", err)
		}
		if hidden {
			return trainingload.CommunitySeries(ctx, s.activities, userID, start, end)
		}
	}
	metrics, err := s.metrics.ListMetricsForUser(ctx, userID, "", start, end)
	if err != nil {
		return nil, fmt.Errorf("list metrics: %w", err)
	}
	if port.IsCommunityActivityRead(ctx) {
		metrics = slices.DeleteFunc(metrics, func(m domain.Metric) bool { return !slices.Contains(domain.AllTrainingLoadKeys, m.Key) })
	}
	return domain.TrainingLoadSeries(metrics), nil
}

func (s *Stats) trainingLoadBounds(days int) (time.Time, time.Time) {
	if days <= 0 || days > TrainingLoadMaxDays {
		days = TrainingLoadDefaultDays
	}
	end := s.now().UTC().Add(24 * time.Hour)
	return end.AddDate(0, 0, -days), end
}

func (s *Stats) TrainingStressCoverage(ctx context.Context, userID domain.UserID, days int) (port.TrainingStressCoverage, error) {
	start, end := s.trainingLoadBounds(days)
	return s.activities.TrainingStressCoverage(ctx, userID, start, end)
}

// BestEffortHistory is one bucket's progression.
type BestEffortHistory struct {
	Items []domain.BestEffortHistoryItem
	Best  float64
}

// BestEffortHistory lists the progression of one (type, metric, window).
func (s *Stats) BestEffortHistory(ctx context.Context, userID domain.UserID, activityType, metric, windowKind string, windowValue int) (BestEffortHistory, error) {
	if s.bestEfforts == nil {
		return BestEffortHistory{}, ErrUnavailable
	}
	if activityType == "" || !domain.BestEffortHistoryMetrics[metric] || (windowKind != "distance" && windowKind != "duration") || windowValue <= 0 || domain.RemovedRecordDistance(domain.BestEffortWindowKind(windowKind), windowValue) {
		return BestEffortHistory{}, port.NewActionError(port.ActionErrorInvalid, "type, metric, window_kind (distance|duration) and window_value are required")
	}
	items, err := s.bestEfforts.ListBestEffortHistory(ctx, userID, activityType, metric, windowKind, windowValue)
	if err != nil {
		return BestEffortHistory{}, fmt.Errorf("best-effort history: %w", err)
	}
	return BestEffortHistory{Items: items, Best: domain.BestEffortHistoryBest(items, metric)}, nil
}

// PersonalRecords lists the caller's all-time best per (type, metric, window).
func (s *Stats) PersonalRecords(ctx context.Context, userID domain.UserID) ([]domain.PersonalRecord, error) {
	if s.bestEfforts == nil {
		return nil, ErrUnavailable
	}
	recs, err := s.bestEfforts.ListPersonalRecords(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("personal records: %w", err)
	}
	return recs, nil
}

// Heatmap bounds: one response holds at most HeatmapMaxActivities tracks
// (newest first); HeatmapBatch sources per geo-track query.
const (
	HeatmapMaxActivities = 5000
	HeatmapBatch         = 500
)

// HeatmapTrack is one activity's track.
type HeatmapTrack struct {
	Activity domain.Activity
	Points   []domain.GeoPoint
}

// HeatmapResult is every track matching a filter.
type HeatmapResult struct {
	Matched     int
	Truncated   bool
	Types       []port.ActivityFacet
	Disciplines []port.ActivityFacet
	Years       []port.ActivityYearStat
	Tracks      []HeatmapTrack
}

// Heatmap lists the member's GPS tracks matching filter at the given resolution.
func (s *Stats) Heatmap(ctx context.Context, userID domain.UserID, filter port.ActivityListFilter, res domain.StreamResolution) (HeatmapResult, error) {
	if s.streams == nil {
		return HeatmapResult{}, ErrUnavailable
	}
	filter.RequireVisibleMap = true
	filter.Sort = "date_desc"
	filter.Limit = HeatmapMaxActivities
	filter.Offset = 0
	var out HeatmapResult
	var err error
	out.Types, out.Disciplines, _, err = s.activities.ActivityFacets(ctx, userID, filter)
	if err != nil {
		return out, fmt.Errorf("activity facets: %w", err)
	}
	acts, matched, err := s.activities.ListActivitiesFiltered(ctx, userID, filter)
	if err != nil {
		return out, fmt.Errorf("filtered activities: %w", err)
	}
	out.Matched, out.Truncated = matched, matched > len(acts)
	out.Years, _ = s.activities.ActivityYearStats(ctx, userID)
	withStream := make([]domain.Activity, 0, len(acts))
	for _, a := range acts {
		if a.PrimaryStreamSourceID != nil {
			withStream = append(withStream, a)
		}
	}
	var zones []domain.PrivacyZone
	if port.IsCommunityActivityRead(ctx) && s.PrivacyZones != nil {
		zones, err = s.PrivacyZones.ListPrivacyZones(ctx, userID)
		if err != nil {
			return out, fmt.Errorf("load activity privacy zones: %w", err)
		}
	}
	for start := 0; start < len(withStream); start += HeatmapBatch {
		tracks, err := s.trackBatch(ctx, withStream[start:min(start+HeatmapBatch, len(withStream))], res)
		if err != nil {
			return out, err
		}
		for _, track := range tracks {
			if len(zones) > 0 {
				track.Points = slices.DeleteFunc(slices.Clone(track.Points), func(point domain.GeoPoint) bool {
					return domain.PointInAnyZone(zones, point.Lat, point.Lon)
				})
			}
			if len(track.Points) > 1 {
				out.Tracks = append(out.Tracks, track)
			}
		}
	}
	return out, nil
}

// trackBatch fetches one batch of tracks; activities with fewer than two
// points are dropped.
func (s *Stats) trackBatch(ctx context.Context, acts []domain.Activity, res domain.StreamResolution) ([]HeatmapTrack, error) {
	ids := make([]domain.SourceID, 0, len(acts))
	for _, a := range acts {
		ids = append(ids, *a.PrimaryStreamSourceID)
	}
	tracks, err := s.streams.QueryGeoTracks(ctx, ids, res)
	if err != nil {
		return nil, fmt.Errorf("geo tracks: %w", err)
	}
	out := make([]HeatmapTrack, 0, len(acts))
	for _, a := range acts {
		if pts := tracks[*a.PrimaryStreamSourceID]; len(pts) >= 2 {
			out = append(out, HeatmapTrack{Activity: a, Points: pts})
		}
	}
	return out, nil
}
