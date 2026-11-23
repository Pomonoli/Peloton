package besteffort

import (
	"math"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// Estimate the longer end of the recording cadence so irregular smart
// recordings can include occasional long intervals. Dense recordings still
// break at short gaps, and gaps over 30 seconds always break the section.
func climbingGapLimit(samples []domain.StreamSample) float64 {
	const maxGap = 30
	var intervals [maxGap]int
	var count int
	for i := 1; i < len(samples); i++ {
		if domain.ContinuousRecordingLeg(samples[i-1], samples[i], maxGap) {
			dt := samples[i].Timestamp.Sub(samples[i-1].Timestamp).Seconds()
			intervals[int(math.Ceil(dt))-1]++
			count++
		}
	}
	// Use the 90th percentile of observed intervals.
	target := count - count/10
	for i, n := range intervals {
		target -= n
		if target <= 0 {
			return float64(min(maxGap, max(5, 3*(i+1))))
		}
	}
	return maxGap
}

func climbingSections(samples []domain.StreamSample, visit func(time.Time, []cyclingPoint)) {
	gap := climbingGapLimit(samples)
	for start := 0; start < len(samples); {
		if !domain.ValidRecordingAltitude(samples[start]) || samples[start].Timestamp.IsZero() {
			start++
			continue
		}
		end := start + 1
		for end < len(samples) && climbingLeg(samples[end-1], samples[end], gap) {
			end++
		}
		if end-start > 1 {
			visit(samples[start].Timestamp, climbingPoints(samples[start:end]))
		}
		start = end
	}
}

func climbingLeg(a, b domain.StreamSample, gap float64) bool {
	// Break at abrupt sensor jumps, including implausible drops that would
	// otherwise set a spurious low point for the next ascent.
	const maxVerticalSpeed = 8.0 // m/s
	return domain.ValidRecordingAltitude(b) && domain.ContinuousRecordingLeg(a, b, gap) &&
		math.Abs(*b.AltitudeM-*a.AltitudeM) <= maxVerticalSpeed*b.Timestamp.Sub(a.Timestamp).Seconds()
}

// Count each climb once it rises beyond the noise band. Track peaks and
// troughs with the same threshold so small oscillations on a climb are not
// counted twice. Credit confirmed gain at the samples where it occurred.
func climbingPoints(samples []domain.StreamSample) []cyclingPoint {
	const noiseBandM = 3.0
	points := make([]cyclingPoint, len(samples))
	altitude := func(i int) float64 { return *samples[i].AltitudeM }
	addGain := func(low, high int) {
		top := altitude(low)
		for i := low + 1; i <= high; i++ {
			next := max(top, altitude(i))
			points[i].metres = next - top
			top = next
		}
	}
	low, high := 0, 0
	for i := 1; i < len(samples); i++ {
		z := altitude(i)
		switch {
		case z <= altitude(high)-noiseBandM && altitude(high)-altitude(low) >= noiseBandM:
			addGain(low, high)
			low, high = i, i
		case z < altitude(low):
			low, high = i, i
		case z >= altitude(high):
			high = i
		}
	}
	if altitude(high)-altitude(low) >= noiseBandM {
		addGain(low, high)
	}
	for i := 1; i < len(points); i++ {
		points[i].metres += points[i-1].metres
		points[i].seconds = samples[i].Timestamp.Sub(samples[0].Timestamp).Seconds()
	}
	return points
}
