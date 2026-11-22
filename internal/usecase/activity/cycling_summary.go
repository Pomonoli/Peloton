package activity

import (
	"context"
	"errors"
	"fmt"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// RefreshCyclingSummary rebuilds motion statistics without re-merging sources.
func RefreshCyclingSummary(ctx context.Context, reader port.ActivityReader, streams port.StreamRepo, store port.CyclingSummaryStore, id domain.ActivityID) error {
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
	if err := applyCyclingSummary(ctx, streams, &act); err != nil {
		return err
	}
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
func applyCyclingSummary(ctx context.Context, streams port.StreamRepo, act *domain.Activity) error {
	if streams == nil || act.Type != domain.ActivityTypeRide {
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
	if hasDistanceSource {
		motion, err := cyclingMotion(ctx, streams, distanceSource)
		if err != nil {
			return err
		}
		applyCyclingDistance(act, motion)
		if hasSpeedSource && speedSource == distanceSource {
			applyCyclingSpeed(act, motion)
			return nil
		}
	}
	if hasSpeedSource {
		motion, err := cyclingMotion(ctx, streams, speedSource)
		if err != nil {
			return err
		}
		applyCyclingSpeed(act, motion)
	}
	return nil
}

func applyCyclingDistance(act *domain.Activity, motion domain.CyclingStream) {
	if motion.HasMotion {
		distance, _, _ := motion.SpeedSummary()
		act.Summary.DistanceM = &distance
	}
}

func cyclingMotion(ctx context.Context, streams port.StreamRepo, id domain.SourceID) (domain.CyclingStream, error) {
	raw, err := streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: id, Resolution: domain.StreamResolutionRaw})
	if err != nil {
		return domain.CyclingStream{}, fmt.Errorf("read cycling summary stream %s: %w", id, err)
	}
	return domain.ValidateCyclingStream(raw.Samples), nil
}

func applyCyclingSpeed(act *domain.Activity, motion domain.CyclingStream) {
	if !motion.HasMotion {
		return
	}
	distance, seconds, peak := motion.SpeedSummary()
	act.Summary.AvgSpeedMps, act.Summary.MaxSpeedMps = nil, nil
	if seconds > 0 {
		act.Summary.AvgSpeedMps, act.Summary.MaxSpeedMps = new(distance/seconds), &peak
	}
}
