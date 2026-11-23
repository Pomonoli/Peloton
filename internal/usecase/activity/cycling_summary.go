package activity

import (
	"context"
	"errors"
	"fmt"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// RefreshCyclingSummary rebuilds motion statistics without re-merging sources.
func RefreshCyclingSummary(ctx context.Context, reader port.ActivityReader, streams port.StreamRepo, store port.CyclingSummaryStore, overrides port.ClassificationOverrideRepo, id domain.ActivityID) error {
	act, err := reader.GetActivity(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if act.IsDeleted() || act.Type != domain.ActivityTypeRide {
		return nil
	}
	before := act.Summary
	overlay, err := loadClassificationOverride(ctx, overrides, id)
	if err != nil {
		return err
	}
	if err := applyCyclingSummary(newSummaryStreamReader(ctx, streams), &act, overlay.DistanceM == nil); err != nil {
		return err
	}
	act.RecalculateAverageSpeed()
	if sameMetric(before.DistanceM, act.Summary.DistanceM) && sameMetric(before.AvgSpeedMps, act.Summary.AvgSpeedMps) &&
		sameMetric(before.MaxSpeedMps, act.Summary.MaxSpeedMps) {
		return nil
	}
	return store.SaveCyclingSummary(ctx, act)
}

func sameMetric(a, b *float64) bool {
	return a == b || (a != nil && b != nil && *a == *b)
}

// applyCyclingSummary validates measurements from the same source selected
// by the activity merge. A summary-only source retains its supplied values.
func applyCyclingSummary(load summaryStreamReader, act *domain.Activity, deriveDistance bool) error {
	if act.Type != domain.ActivityTypeRide {
		return nil
	}
	distanceSource, hasDistanceSource := act.MergeProvenance.Winner(domain.FieldGroupDistance)
	speedSource, hasSpeedSource := act.MergeProvenance.Winner(domain.FieldGroupSpeed)
	if act.PrimaryStreamSourceID != nil {
		if !hasDistanceSource {
			distanceSource, hasDistanceSource = *act.PrimaryStreamSourceID, true
		}
		if !hasSpeedSource {
			speedSource, hasSpeedSource = *act.PrimaryStreamSourceID, true
		}
	}
	if hasDistanceSource && deriveDistance {
		samples, err := load(distanceSource)
		if err != nil {
			return err
		}
		motion := domain.ValidateCyclingStream(samples)
		applyCyclingDistance(act, motion)
		if hasSpeedSource && speedSource == distanceSource {
			applyCyclingMaxSpeed(act, motion)
			return nil
		}
	}
	if hasSpeedSource {
		samples, err := load(speedSource)
		if err != nil {
			return err
		}
		applyCyclingMaxSpeed(act, domain.ValidateCyclingStream(samples))
	}
	return nil
}

func applyCyclingDistance(act *domain.Activity, motion domain.CyclingStream) {
	if motion.HasMotion {
		distance, _, _ := motion.SpeedSummary()
		act.Summary.DistanceM = &distance
	}
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
