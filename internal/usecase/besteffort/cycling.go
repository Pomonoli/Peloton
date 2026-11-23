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
		origin = firstRecordingTime(samples)
	}
	validated := domain.ValidateCyclingStream(samples)
	best := make([]domain.BestEffort, len(windows))
	verified := make([]domain.BestEffort, len(windows))
	validated.Sections(func(validatedSectionStart, end int) {
		section := samples[validatedSectionStart:end]
		points := motionPoints(validated.Legs[validatedSectionStart:end])
		for i, w := range windows {
			var effort domain.BestEffort
			var ok bool
			switch {
			case w.Metric == domain.BestEffortMetricSpeed || w.Metric == domain.BestEffortMetricPace:
				effort, ok = uc.cyclingMotionEffort(act, src, section[0].Timestamp, points, w, now, nil)
			}
			if ok && betterCyclingEffort(effort, best[i]) {
				effort.StartOffset = int(effort.Timestamp.Sub(origin).Seconds())
				best[i] = effort
			}
		}
		for start := 0; start < len(section); {
			for start < len(section) && !verifiedElevation(section[start]) {
				start++
			}
			if start == len(section) {
				break
			}
			end := start + 1
			for end < len(section) && verifiedElevation(section[end]) &&
				validated.Legs[validatedSectionStart+end].HasGPS {
				end++
			}
			if end-start >= 2 {
				local := make([]cyclingPoint, end-start)
				for i := range local {
					local[i] = cyclingPoint{points[start+i].seconds - points[start].seconds, points[start+i].metres - points[start].metres}
				}
				profile := newDownhillProfile(section[start:end], local)
				for i, w := range windows {
					if w.Metric != domain.BestEffortMetricSpeed && w.Metric != domain.BestEffortMetricPace {
						continue
					}
					effort, ok := uc.cyclingMotionEffort(act, src, section[start].Timestamp, local, w, now, &profile)
					if ok && betterCyclingEffort(effort, verified[i]) {
						effort.DownhillExcluded = true
						effort.StartOffset = int(effort.Timestamp.Sub(origin).Seconds())
						verified[i] = effort
					}
				}
			}
			start = end
		}
	})
	for i, w := range windows {
		if w.Kind != domain.BestEffortWindowDuration || w.Metric == domain.BestEffortMetricSpeed {
			continue
		}
		if effort, ok := uc.timedSensorEffort(act, src, samples, w, now); ok {
			effort.StartOffset = int(effort.Timestamp.Sub(origin).Seconds())
			best[i] = effort
		}
	}
	var out []domain.BestEffort
	for _, effort := range append(best, verified...) {
		if effort.WindowValue > 0 {
			out = append(out, effort)
		}
	}
	return out
}

func motionPoints(legs []domain.CyclingLeg) []cyclingPoint {
	points := make([]cyclingPoint, len(legs))
	for i := 1; i < len(legs); i++ {
		leg := legs[i]
		points[i] = cyclingPoint{points[i-1].seconds + leg.Seconds, points[i-1].metres + leg.DistanceM}
	}
	return points
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
	act domain.Activity, src domain.ActivitySource, start time.Time, points []cyclingPoint, w windowSpec, now time.Time, profile *downhillProfile,
) (domain.BestEffort, bool) {
	byDistance := w.Kind == domain.BestEffortWindowDistance && w.Metric == domain.BestEffortMetricPace
	if !byDistance && (w.Kind != domain.BestEffortWindowDuration || w.Metric != domain.BestEffortMetricSpeed) {
		return domain.BestEffort{}, false
	}
	var offset, elapsed, distance float64
	if profile == nil {
		offset, elapsed, distance = fastestCyclingWindow(points, float64(w.Value), byDistance)
	} else {
		offset, elapsed, distance = profile.fastestWindow(float64(w.Value), byDistance)
	}
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

// Without eligibility constraints, an optimum has at least one endpoint on a sample.
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
					fraction := (target - xPrev) / (xEnd - xPrev)
					yTarget := yPrev + (yEnd-yPrev)*fraction
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
