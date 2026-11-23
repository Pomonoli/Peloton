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
	var points []cyclingPoint
	var start time.Time
	var previous float64
	var lastIndex int32
	finish := func() {
		offset, _, amount := fastestCyclingWindow(points, width, false)
		value := amount / width
		if w.Metric == domain.BestEffortMetricVAM {
			value *= 3600
		}
		if value > bestValue {
			bestValue = value
			bestStart = start.Add(time.Duration(offset * float64(time.Second)))
		}
		points = points[:0]
	}
	for _, sample := range samples {
		value, ok := readMetric(sample, w.Metric)
		if w.Metric == domain.BestEffortMetricVAM {
			ok = sample.AltitudeM != nil && *sample.AltitudeM >= -450 && *sample.AltitudeM <= 9000
			if ok {
				value = *sample.AltitudeM
			}
		}
		if !ok {
			finish()
			continue
		}
		if len(points) > 0 {
			dt := sample.Timestamp.Sub(start).Seconds() - points[len(points)-1].seconds
			if dt <= 0 || dt > 15 || (lastIndex > 0 && sample.RecordingIndex != lastIndex+1) {
				finish()
			}
		}
		lastIndex = sample.RecordingIndex
		if len(points) == 0 {
			start = sample.Timestamp
			points = append(points, cyclingPoint{})
			previous = value
			continue
		}
		last := points[len(points)-1]
		seconds := sample.Timestamp.Sub(start).Seconds()
		amount := previous * (seconds - last.seconds)
		if w.Metric == domain.BestEffortMetricVAM {
			amount = value - previous
		}
		points = append(points, cyclingPoint{seconds, last.metres + amount})
		previous = value
	}
	finish()
	if bestValue <= 0 {
		return domain.BestEffort{}, false
	}
	return domain.BestEffort{
		ID: domain.BestEffortID(uc.newID()), ActivityID: act.ID, ActivitySourceID: src.ID, UserID: act.UserID,
		ActivityType: act.Type, Discipline: act.Discipline, Metric: w.Metric, WindowKind: w.Kind, WindowValue: w.Value,
		AchievedValue: bestValue, StartOffset: int(bestStart.Sub(samples[0].Timestamp).Seconds()), DurationS: width, Timestamp: bestStart, CreatedAt: now,
	}, true
}
