package activity

import (
	"context"
	"errors"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func canEstimateMovingTime(act *domain.Activity) bool {
	return act.ElapsedDuration > 0 && (act.Type == domain.ActivityTypeRide || act.Type == domain.ActivityTypeWorkout)
}

func needsGPSMovingTime(act *domain.Activity, source domain.ActivitySource) bool {
	if !canEstimateMovingTime(act) || source.Status == domain.SourceStatusDetached {
		return false
	}
	// Untyped GPX uploads use the workout classification. Manual entries
	// and other provider summaries can carry authoritative moving time.
	switch source.Provider {
	case "garmin":
		return act.Type == domain.ActivityTypeRide &&
			(source.Parsed.MovingDuration >= source.Parsed.ElapsedDuration ||
				(source.Parsed.MovingDuration <= 0 && source.SourceWorkerName != "garmin-connect"))
	case "upload":
		return (act.Type == domain.ActivityTypeRide || source.Parsed.Type == domain.ActivityTypeWorkout) &&
			source.Parsed.MovingDuration >= source.Parsed.ElapsedDuration
	default:
		return false
	}
}

func applyGPSMovingTime(act *domain.Activity, samples []domain.StreamSample) bool {
	if moving, ok := domain.EstimateGPSMovingTime(samples, act.ElapsedDuration, nil); ok {
		act.MovingDuration = moving
		return true
	}
	return false
}

func applyMovingTime(ctx context.Context, act *domain.Activity, sources []domain.ActivitySource, load summaryStreamReader, archive port.ActivityTimingArchive) error {
	winner, ok := act.MergeProvenance.Winner(domain.FieldGroupDuration)
	if !ok {
		return nil
	}
	for _, source := range sources {
		if source.ID != winner || !needsGPSMovingTime(act, source) {
			continue
		}
		if archive != nil {
			moving, found, err := archive.FITMovingTime(ctx, source)
			if err != nil {
				return err
			}
			if found {
				act.MovingDuration = min(moving, act.ElapsedDuration).Truncate(time.Second)
				return nil
			}
		}
		samples, err := load(source.ID)
		if err != nil {
			return err
		}
		if !applyGPSMovingTime(act, samples) && archive != nil {
			moving, found, err := archive.GPXMovingTime(ctx, source)
			if err != nil {
				return err
			}
			if found {
				act.MovingDuration = min(moving, act.ElapsedDuration).Truncate(time.Second)
			}
		}
		return nil
	}
	return nil
}

type activityTimingStore interface {
	GetActivity(context.Context, domain.ActivityID) (domain.Activity, error)
	ListSourcesForActivity(context.Context, domain.ActivityID) ([]domain.ActivitySource, error)
	SaveActivityTiming(context.Context, domain.Activity) error
}

// refreshActivityTiming updates canonical moving time and its derived metrics.
// The caller serializes this with owner activity updates.
func refreshActivityTiming(ctx context.Context, store activityTimingStore, streams port.StreamRepo, overrides port.ClassificationOverrideRepo, archive port.ActivityTimingArchive, id domain.ActivityID) (bool, error) {
	act, err := store.GetActivity(ctx, id)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && act.IsDeleted()) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	beforeMoving, beforeSpeed := act.MovingDuration, act.Summary.AvgSpeedMps
	if canEstimateMovingTime(&act) && !act.ProviderRestricted {
		overlay, err := loadClassificationOverride(ctx, overrides, id)
		if err != nil {
			return false, err
		}
		if overlay.MovingDuration == nil {
			sources, err := store.ListSourcesForActivity(ctx, id)
			if err != nil {
				return false, err
			}
			canonical, _ := domain.CanonicalSources(sources)
			if err := applyMovingTime(ctx, &act, canonical, newSummaryStreamReader(ctx, streams), archive); err != nil {
				return false, err
			}
			refreshEstimatedTrainingStress(&act, beforeMoving, canonical)
		}
	}
	act.RecalculateAverageSpeed()
	if act.MovingDuration == beforeMoving && sameMetric(beforeSpeed, act.Summary.AvgSpeedMps) {
		return false, nil
	}
	if err := store.SaveActivityTiming(ctx, act); err != nil {
		return false, err
	}
	return true, nil
}

func refreshEstimatedTrainingStress(act *domain.Activity, before time.Duration, sources []domain.ActivitySource) {
	if act.MovingDuration == before || before <= 0 || act.Summary.TSS == nil {
		return
	}
	winner, _ := act.MergeProvenance.Winner(domain.FieldGroupTrainingLoad)
	for _, source := range sources {
		if source.ID == winner && source.Parsed.Summary.TSS != nil {
			return
		}
	}
	// Estimated TSS is proportional to moving time. Preserve the intensity
	// and athlete thresholds used for the original estimate.
	if act.MovingDuration <= 0 {
		act.Summary.TSS = nil
		return
	}
	act.Summary.TSS = new(*act.Summary.TSS * act.MovingDuration.Seconds() / before.Seconds())
}
