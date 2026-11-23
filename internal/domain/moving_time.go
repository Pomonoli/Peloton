package domain

import "time"

// RecordingInterval is an explicitly active portion of a device timer.
type RecordingInterval struct {
	Start, End time.Time
}

// EstimateGPSMovingTime estimates time in motion from timestamped GPS fixes.
// When provided, active intervals must be ordered and non-overlapping.
func EstimateGPSMovingTime(samples []StreamSample, duration time.Duration, active []RecordingInterval) (time.Duration, bool) {
	if duration < 6*time.Second || len(samples) < 3 {
		return 0, false
	}
	gps := CyclingStream{Legs: make([]CyclingLeg, len(samples))}
	maxGap := cyclingGapLimit(samples)
	for i := 1; i < len(samples); i++ {
		a, b := samples[i-1], samples[i]
		if !ContinuousRecordingLeg(a, b, maxGap) {
			continue
		}
		from, fromOK := cyclingPosition(a)
		to, toOK := cyclingPosition(b)
		if !fromOK || !toOK {
			continue
		}
		dt := b.Timestamp.Sub(a.Timestamp).Seconds()
		distance := HaversineMeters(from, to)
		if validCyclingSpeed(distance / dt) {
			gps.Legs[i] = CyclingLeg{Seconds: dt, DistanceM: distance, Valid: true, HasMotion: true, HasGPS: true}
		}
	}
	// A quick change at a stop can be credible when motion continues on
	// either side. Reject round trips and impossible speeds for timing.
	gps.removeSpikes(samples, false)

	var moving, observed time.Duration
	var coveredUntil time.Time
	timer := 0
	gps.Sections(func(start, end int) {
		var inMotion bool
		left, right := start, start
		stillUntil := start
		var sustainedStop bool
		var path float64
		for i := start + 1; i < end; i++ {
			if i > stillUntil {
				stillUntil = i - 1
				for stillUntil+1 < end && gps.Legs[stillUntil+1].DistanceM <= .3*gps.Legs[stillUntil+1].Seconds {
					stillUntil++
				}
				sustainedStop = samples[stillUntil].Timestamp.Sub(samples[i-1].Timestamp) >= 5*time.Second
			}
			// Smooth around the interval midpoint. Use time, so a sparse
			// track and a 1 Hz track have comparable stop detection.
			mid := samples[i-1].Timestamp.Add(samples[i].Timestamp.Sub(samples[i-1].Timestamp) / 2)
			for right < i || (right+1 < end && samples[right+1].Timestamp.Sub(mid) <= 4*time.Second) {
				right++
				path += gps.Legs[right].DistanceM
			}
			for left < i-1 && mid.Sub(samples[left].Timestamp) > 4*time.Second {
				left++
				path -= gps.Legs[left].DistanceM
			}
			inMotion = !sustainedStop && movingGPSWindow(samples[left], samples[right], path, inMotion)
			endTime := samples[i].Timestamp
			if !endTime.After(coveredUntil) {
				continue
			}
			startTime := laterTime(samples[i-1].Timestamp, coveredUntil)
			dt := activeLegDuration(startTime, endTime, active, &timer)
			coveredUntil = endTime
			observed += dt
			if inMotion {
				moving += dt
			}
		}
	})

	// A few points inside a much longer activity cannot replace source time.
	// Small gaps are omitted, and enough coverage must remain to estimate.
	if observed < 6*time.Second || observed.Seconds() < .7*duration.Seconds() ||
		observed > duration+time.Second {
		return 0, false
	}
	return min(moving.Round(time.Second), duration.Truncate(time.Second)), true
}

func movingGPSWindow(a, b StreamSample, path float64, inMotion bool) bool {
	from, _ := cyclingPosition(a)
	to, _ := cyclingPosition(b)
	net := HaversineMeters(from, to)
	seconds := b.Timestamp.Sub(a.Timestamp).Seconds()
	// Require coherent displacement to start moving. While moving, allow
	// more noise and a lower speed, including slow climbing and turns.
	if inMotion {
		return net >= max(1.5, .4*seconds) && (net >= .35*path || net >= 8)
	}
	return net >= max(2.5, .6*seconds) && (net >= .55*path || net >= 8)
}

func activeLegDuration(start, end time.Time, active []RecordingInterval, next *int) time.Duration {
	if len(active) == 0 {
		return end.Sub(start)
	}
	var total time.Duration
	for *next < len(active) && active[*next].Start.Before(end) {
		window := active[*next]
		total += max(0, earlierTime(end, window.End).Sub(laterTime(start, window.Start)))
		if window.End.After(end) {
			break
		}
		(*next)++
	}
	return total
}

func earlierTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func laterTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
