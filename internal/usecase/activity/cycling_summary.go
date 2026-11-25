package activity

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// RefreshCyclingSummary rebuilds motion statistics without re-merging sources.
func RefreshCyclingSummary(ctx context.Context, reader port.ActivityAggregateReader, streams port.StreamRepo, store port.CyclingSummaryStore, overrides port.ClassificationOverrideRepo, id domain.ActivityID) (bool, error) {
	act, err := reader.GetActivity(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if act.IsDeleted() || act.Type != domain.ActivityTypeRide {
		return false, nil
	}
	before := act.Summary
	overlay, err := loadClassificationOverride(ctx, overrides, id)
	if err != nil {
		return false, err
	}
	sources, err := reader.ListSourcesForActivity(ctx, id)
	if err != nil {
		return false, err
	}
	canonical, _ := domain.CanonicalSources(sources)
	if err := applyCyclingSummary(newSummaryStreamReader(ctx, streams), &act, canonical, overlay.DistanceM == nil); err != nil {
		return false, err
	}
	if overlay.DistanceM != nil {
		act.Summary.DistanceM = overlay.DistanceM
	} else if act.Summary.DistanceM == nil {
		// A historical row can have a total even if its source lacks both
		// usable motion and a total.
		act.Summary.DistanceM = before.DistanceM
	}
	act.RecalculateAverageSpeed()
	if sameMetric(before.DistanceM, act.Summary.DistanceM) && sameMetric(before.AvgSpeedMps, act.Summary.AvgSpeedMps) &&
		sameMetric(before.MaxSpeedMps, act.Summary.MaxSpeedMps) {
		return false, nil
	}
	if err := store.SaveCyclingSummary(ctx, act); err != nil {
		return false, err
	}
	return true, nil
}

func sameMetric(a, b *float64) bool {
	return a == b || (a != nil && b != nil && *a == *b)
}

// applyCyclingSummary validates measurements from the same source selected
// by the activity merge. A summary-only source retains its supplied values.
func applyCyclingSummary(load summaryStreamReader, act *domain.Activity, sources []domain.ActivitySource, deriveDistance bool) error {
	if act.Type != domain.ActivityTypeRide {
		return nil
	}
	distanceSource, hasDistanceSource := act.MergeProvenance.Winner(domain.FieldGroupDistance)
	speedSource, hasSpeedSource := act.MergeProvenance.Winner(domain.FieldGroupSpeed)
	if act.PrimaryStreamSourceID != nil {
		if !hasDistanceSource {
			distanceSource = *act.PrimaryStreamSourceID
		}
		if !hasSpeedSource {
			speedSource, hasSpeedSource = *act.PrimaryStreamSourceID, true
		}
	}
	distanceIndex := slices.IndexFunc(sources, func(source domain.ActivitySource) bool { return source.ID == distanceSource })
	if deriveDistance && distanceIndex >= 0 {
		samples, err := load(distanceSource)
		if err != nil {
			return err
		}
		motion := domain.ValidateCyclingStream(samples)
		act.Summary.DistanceM = domain.CyclingActivityDistance(samples, motion, sources[distanceIndex].Parsed.Summary.DistanceM)
		if speedSource == distanceSource {
			applyCyclingMaxSpeed(act, motion)
			return nil
		}
	}
	if !hasSpeedSource {
		return nil
	}
	samples, err := load(speedSource)
	if err != nil {
		return err
	}
	applyCyclingMaxSpeed(act, domain.ValidateCyclingStream(samples))
	return nil
}

type summaryStreamReader func(domain.SourceID) ([]domain.StreamSample, error)

func newSummaryStreamReader(ctx context.Context, streams port.StreamRepo) summaryStreamReader {
	cached := make(map[domain.SourceID][]domain.StreamSample)
	return func(id domain.SourceID) ([]domain.StreamSample, error) {
		if samples, ok := cached[id]; ok || streams == nil {
			return samples, nil
		}
		raw, err := streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: id, Resolution: domain.StreamResolutionRaw})
		if err != nil {
			return nil, fmt.Errorf("read activity summary stream %s: %w", id, err)
		}
		cached[id] = raw.Samples
		return raw.Samples, nil
	}
}

func applyCyclingMaxSpeed(act *domain.Activity, motion domain.CyclingStream) {
	if !motion.HasMotion {
		return
	}
	_, seconds, peak := motion.SpeedSummary()
	if seconds > 0 {
		act.Summary.MaxSpeedMps = &peak
	}
}
