package besteffort

import (
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// Use the actual recorded times, with no interpolation across missing readings.
// Integrating each channel as a piecewise constant rate handles smart recording
// and burst sampling without shortening or lengthening the requested window.
func (uc *ComputeBestEffortsForSource) timedSensorEffort(
	act domain.Activity, src domain.ActivitySource, samples []domain.StreamSample,
	w windowSpec, now time.Time,
) (domain.BestEffort, bool) {
	var bestValue float64
	var bestStart time.Time
	width := float64(w.Value)
	consider := func(start time.Time, points []cyclingPoint) {
		offset, _, amount := fastestCyclingWindow(points, width, false)
		value := amount / width
		if w.Metric == domain.BestEffortMetricVAM {
			value *= 3600
		}
		if value > bestValue {
			bestValue = value
			bestStart = start.Add(time.Duration(offset * float64(time.Second)))
		}
	}
	if w.Metric == domain.BestEffortMetricVAM {
		climbingSections(samples, consider)
	} else {
		sensorSections(samples, w.Metric, consider)
	}
	if bestValue <= 0 {
		return domain.BestEffort{}, false
	}
	origin := firstRecordingTime(samples)
	return domain.BestEffort{
		ID: domain.BestEffortID(uc.newID()), ActivityID: act.ID, ActivitySourceID: src.ID, UserID: act.UserID,
		ActivityType: act.Type, Discipline: act.Discipline, Metric: w.Metric, WindowKind: w.Kind, WindowValue: w.Value,
		AchievedValue: bestValue, StartOffset: int(bestStart.Sub(origin).Seconds()), DurationS: width, Timestamp: bestStart, CreatedAt: now,
	}, true
}

func sensorSections(samples []domain.StreamSample, metric domain.BestEffortMetric, visit func(time.Time, []cyclingPoint)) {
	var points []cyclingPoint
	var start time.Time
	var previous domain.StreamSample
	var previousValue float64
	for _, sample := range samples {
		value, ok := readMetric(sample, metric)
		if len(points) > 0 && (!ok || !domain.ContinuousRecordingLeg(previous, sample, 15)) {
			visit(start, points)
			points = points[:0]
		}
		if !ok || sample.Timestamp.IsZero() {
			continue
		}
		if len(points) == 0 {
			start = sample.Timestamp
			points = append(points, cyclingPoint{})
		} else {
			last := points[len(points)-1]
			seconds := sample.Timestamp.Sub(start).Seconds()
			amount := previousValue * (seconds - last.seconds)
			points = append(points, cyclingPoint{seconds, last.metres + amount})
		}
		previous, previousValue = sample, value
	}
	visit(start, points)
}

func firstRecordingTime(samples []domain.StreamSample) time.Time {
	for _, sample := range samples {
		if !sample.Timestamp.IsZero() {
			return sample.Timestamp
		}
	}
	return time.Time{}
}
