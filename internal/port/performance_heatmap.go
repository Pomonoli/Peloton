package port

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/heatmap"
)

// HeatmapActivity references a merged activity and the version of its
// attached streams. The version is opaque to the use case.
type HeatmapActivity struct {
	ID               domain.ActivityID
	Primary          domain.SourceID
	Sources          []domain.SourceID
	Version          string
	Ready, WithTrack bool
}

type HeatmapViewport struct {
	Level heatmap.Level
	// Nil bounds mean the complete map. West > East wraps the date line.
	Bounds *domain.CatalogSegmentBounds
}

// PerformanceHeatmapRepo stores reusable activity-level bin contributions.
type PerformanceHeatmapRepo interface {
	HeatmapActivities(ctx context.Context, member domain.UserID, filter ActivityListFilter) ([]HeatmapActivity, error)
	HeatmapSamples(ctx context.Context, source domain.SourceID, gps bool) ([]domain.StreamSample, error)
	SaveHeatmapBins(ctx context.Context, activity HeatmapActivity, bins [3][]heatmap.Bin) error
	VisitHeatmapBins(ctx context.Context, member domain.UserID, activities []HeatmapActivity, viewport HeatmapViewport, visit func(heatmap.Bin)) error
}
