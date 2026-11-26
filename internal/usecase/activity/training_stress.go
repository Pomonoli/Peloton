package activity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// estimateTrainingStress runs after the canonical summary and moving time are
// final. A valid score supplied by the selected source takes precedence.
func estimateTrainingStress(ctx context.Context, act *domain.Activity, athlete port.AthleteProfileRepo) error {
	if domain.ValidTrainingStress(act.Summary.TSS) {
		return nil
	}
	act.Summary.TSS = nil
	// The canonical activity stores moving duration in whole seconds.
	moving := act.MovingDuration.Truncate(time.Second)
	if moving <= 0 {
		return nil
	}
	var thresholds domain.AthleteThresholds
	if athlete != nil && domain.HasTrainingStressSensor(act.Summary) {
		entries, err := athlete.ListEntries(ctx, act.UserID)
		if err != nil {
			return fmt.Errorf("load athlete thresholds for training stress: %w", err)
		}
		thresholds = domain.NewAthleteProfile(entries).ThresholdsAt(act.StartTime)
	}
	score, intensity, _ := domain.EstimateTSS(act.Summary, moving, thresholds)
	act.Summary.TSS = &score
	act.Summary.IntensityFactor = &intensity
	delete(act.MergeProvenance, domain.FieldGroupTrainingLoad)
	return nil
}

func (uc *RecomputeActivityFromSources) refreshTrainingStress(ctx context.Context, act *domain.Activity, sources []domain.ActivitySource) error {
	canonical, _ := domain.CanonicalSources(sources)
	winner, _ := act.MergeProvenance.Winner(domain.FieldGroupTrainingLoad)
	previous := act.Summary.TSS
	act.Summary.TSS, act.Summary.IntensityFactor = nil, nil
	hasImported := false
	foundWinner := false
	var legacy *domain.ActivitySource
	for _, src := range canonical {
		if src.Status == domain.SourceStatusDetached {
			continue
		}
		if src.ID == winner {
			foundWinner = true
			src.Parsed.ApplyTo(domain.FieldGroupTrainingLoad, act)
			if domain.ValidTrainingStress(act.Summary.TSS) {
				return nil
			}
		}
		if domain.ValidTrainingStress(src.Parsed.Summary.TSS) {
			hasImported = true
			if legacy == nil && sameMetric(previous, src.Parsed.Summary.TSS) {
				legacy = &src
			}
		}
	}
	if !foundWinner {
		delete(act.MergeProvenance, domain.FieldGroupTrainingLoad)
		if legacy != nil {
			// Older rows may lack provenance for a valid imported score.
			applyImportedTrainingStress(act, *legacy)
			return nil
		}
	}
	if hasImported {
		overrides, err := uc.loadFieldOverrides(ctx, act.ID)
		if err != nil {
			return err
		}
		if src, ok := domain.TrainingStressSource(canonical, uc.buildResolver(ctx, act.UserID)(act.Type), overrides); ok {
			applyImportedTrainingStress(act, src)
		}
	}
	return estimateTrainingStress(ctx, act, uc.athlete)
}

func applyImportedTrainingStress(act *domain.Activity, src domain.ActivitySource) {
	src.Parsed.ApplyTo(domain.FieldGroupTrainingLoad, act)
	if act.MergeProvenance == nil {
		act.MergeProvenance = domain.MergeProvenance{}
	}
	act.MergeProvenance.Set(domain.FieldGroupTrainingLoad, src.ID)
}

type TrainingStressRepairResult struct {
	Changed, Missing, MissingWithDuration bool
}

// RepairTrainingStress refreshes the canonical score without rewriting the
// source or other activity fields. The caller holds the owner's update lock.
func (uc *RecomputeActivityFromSources) RepairTrainingStress(ctx context.Context, store port.TrainingStressStore, id domain.ActivityID) (TrainingStressRepairResult, error) {
	act, err := uc.activities.GetActivity(ctx, id)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && act.IsDeleted()) {
		return TrainingStressRepairResult{}, nil
	}
	if err != nil {
		return TrainingStressRepairResult{}, err
	}
	sources, err := uc.activities.ListSourcesForActivity(ctx, id)
	if err != nil {
		return TrainingStressRepairResult{}, err
	}
	before := act.Summary
	winner, _ := act.MergeProvenance.Winner(domain.FieldGroupTrainingLoad)
	if err := uc.refreshTrainingStress(ctx, &act, sources); err != nil {
		return TrainingStressRepairResult{}, err
	}
	next, _ := act.MergeProvenance.Winner(domain.FieldGroupTrainingLoad)
	changed := winner != next || !sameMetric(before.TSS, act.Summary.TSS) ||
		!sameMetric(before.IntensityFactor, act.Summary.IntensityFactor)
	missing := !domain.ValidTrainingStress(act.Summary.TSS)
	result := TrainingStressRepairResult{Changed: changed, Missing: missing,
		MissingWithDuration: missing && act.MovingDuration >= time.Second}
	if changed {
		return result, store.SaveActivityTrainingStress(ctx, act)
	}
	return result, nil
}
