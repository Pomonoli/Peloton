package besteffort

import (
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

type cyclingPoint struct {
	seconds, metres float64
}

func (uc *ComputeBestEffortsForSource) computeCycling(
	act domain.Activity, src domain.ActivitySource, samples []domain.StreamSample, windows []windowSpec, now time.Time,
) []domain.BestEffort {
	if len(samples) < 2 {
		return nil
	}
	origin := act.StartTime
	if !src.Parsed.StartTime.IsZero() {
		origin = src.Parsed.StartTime
	} else if origin.IsZero() {
		origin = samples[0].Timestamp
	}
	validated := domain.ValidateCyclingStream(samples)
	best := make([]domain.BestEffort, len(windows))
	validated.Sections(func(start, end int) {
		section := samples[start:end]
		points := make([]cyclingPoint, len(section))
		for i := 1; i < len(points); i++ {
			leg := validated.Legs[start+i]
			points[i] = cyclingPoint{points[i-1].seconds + leg.Seconds, points[i-1].metres + leg.DistanceM}
		}
		for i, w := range windows {
			var effort domain.BestEffort
			var ok bool
			switch {
			case w.Metric == domain.BestEffortMetricSpeed || w.Metric == domain.BestEffortMetricPace:
				effort, ok = uc.cyclingMotionEffort(act, src, section[0].Timestamp, points, w, now)
			case w.Kind == domain.BestEffortWindowDuration && len(section) >= w.Value:
				effort, ok = uc.computeDurationEffort(act, src, section, w, now)
			}
			if ok && betterCyclingEffort(effort, best[i]) {
				effort.StartOffset = int(effort.Timestamp.Sub(origin).Seconds())
				best[i] = effort
			}
		}
	})
	var out []domain.BestEffort
	for _, effort := range best {
		if effort.WindowValue > 0 {
			out = append(out, effort)
		}
	}
	return out
}

func betterCyclingEffort(candidate, current domain.BestEffort) bool {
	if current.WindowValue == 0 {
		return true
	}
	if candidate.Metric == domain.BestEffortMetricPace {
		return candidate.AchievedValue < current.AchievedValue
	}
	return candidate.AchievedValue > current.AchievedValue
}

func (uc *ComputeBestEffortsForSource) cyclingMotionEffort(
	act domain.Activity, src domain.ActivitySource, start time.Time, points []cyclingPoint, w windowSpec, now time.Time,
) (domain.BestEffort, bool) {
	byDistance := w.Kind == domain.BestEffortWindowDistance && w.Metric == domain.BestEffortMetricPace
	if !byDistance && (w.Kind != domain.BestEffortWindowDuration || w.Metric != domain.BestEffortMetricSpeed) {
		return domain.BestEffort{}, false
	}
	offset, elapsed, distance := fastestCyclingWindow(points, float64(w.Value), byDistance)
	if elapsed <= 0 || distance <= 0 {
		return domain.BestEffort{}, false
	}
	value := distance / elapsed
	if byDistance {
		value = 1000 * elapsed / distance
	}
	return domain.BestEffort{
		ID: domain.BestEffortID(uc.newID()), ActivityID: act.ID, ActivitySourceID: src.ID, UserID: act.UserID,
		ActivityType: act.Type, Discipline: act.Discipline, Metric: w.Metric, WindowKind: w.Kind, WindowValue: w.Value,
		AchievedValue: value, DurationS: elapsed, DistanceM: &distance, Timestamp: start.Add(time.Duration(offset * float64(time.Second))),
		CreatedAt: now,
	}, true
}

// With linear interpolation, an optimum has at least one endpoint on a sample.
// Scan both ways to consider every such endpoint in O(n) per window.
func fastestCyclingWindow(points []cyclingPoint, width float64, byDistance bool) (offset, elapsed, distance float64) {
	if len(points) < 2 || width <= 0 {
		return 0, 0, 0
	}
	last := points[len(points)-1]
	if (byDistance && last.metres < width) || (!byDistance && last.seconds < width) {
		return 0, 0, 0
	}
	for _, direction := range []int{1, -1} {
		index, limit := 0, len(points)
		if direction == -1 {
			index, limit = len(points)-1, -1
		}
		coordinates := func(i int) (float64, float64) {
			p := points[i]
			if byDistance {
				return p.metres * float64(direction), p.seconds * float64(direction)
			}
			return p.seconds * float64(direction), p.metres * float64(direction)
		}
		end := index + direction
		for left := index; end != limit; left += direction {
			x, y := coordinates(left)
			target := x + width
			if end == left {
				end += direction
			}
			for end != limit {
				xEnd, yEnd := coordinates(end)
				if xEnd >= target {
					xPrev, yPrev := coordinates(end - direction)
					yTarget := yPrev + (yEnd-yPrev)*(target-xPrev)/(xEnd-xPrev)
					span := yTarget - y
					if byDistance && span > 0 && (elapsed == 0 || span < elapsed) {
						offset, elapsed, distance = max(y, -yTarget), span, width
					}
					if !byDistance && span > distance {
						offset, elapsed, distance = max(x, -target), width, span
					}
					break
				}
				end += direction
			}
		}
	}
	return offset, elapsed, distance
}
