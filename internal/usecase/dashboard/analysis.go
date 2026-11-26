package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/trainingload"
)

type AnalysisResult struct {
	Start, End   string // Inclusive UTC dates.
	Load         []domain.TrainingLoadDay
	Stress       []port.TrainingStressDay
	Coverage     port.TrainingStressCoverage
	RecentScored int
	RecentTotal  int
}

// Analysis uses the same rider-visible scores for coverage, stress totals and
// training load. The requested range clips the output, leaving the EWMA state intact.
func (s *Stats) Analysis(ctx context.Context, userID domain.UserID, days int) (AnalysisResult, error) {
	now := s.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	end := today.AddDate(0, 0, 1)
	stress, err := s.activities.TrainingStressDays(ctx, userID, end)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("read training stress: %w", err)
	}
	if days <= 0 || days > TrainingLoadMaxDays {
		days = TrainingLoadDefaultDays
	}
	start := end.AddDate(0, 0, -days)
	// The Analysis selector sends the maximum day setting for All.
	if days == TrainingLoadMaxDays {
		start = today
		if len(stress) > 0 {
			start, err = time.Parse(time.DateOnly, stress[0].Date)
			if err != nil {
				return AnalysisResult{}, fmt.Errorf("parse first activity day: %w", err)
			}
		}
	}
	out := AnalysisResult{
		Start: start.Format(time.DateOnly), End: today.Format(time.DateOnly),
		Load: trainingload.RecordedSeries(stress, start, end),
	}
	recent := end.AddDate(0, 0, -42).Format(time.DateOnly)
	for _, day := range stress {
		if day.Date >= recent {
			out.RecentScored += day.Scored()
			out.RecentTotal += day.Scored() + day.Missing
		}
		if day.Date < out.Start {
			continue
		}
		out.Stress = append(out.Stress, day)
		out.Coverage.Imported += day.Imported
		out.Coverage.SensorEstimate += day.SensorEstimate
		out.Coverage.DurationEstimate += day.DurationEstimate
		out.Coverage.Missing += day.Missing
	}
	return out, nil
}
