package domain

import (
	"math"
	"time"
)

// CyclingLeg describes the interval ending at the corresponding raw sample.
// A break has Valid=false. A stationary interval is valid with zero distance.
type CyclingLeg struct {
	Seconds, DistanceM       float64
	Valid, HasMotion, HasGPS bool
}

// CyclingStream holds derived measurements aligned to the original samples.
// Its first leg is always empty.
type CyclingStream struct {
	Legs      []CyclingLeg
	HasMotion bool
}

const (
	maxCyclingSpeed    = 36.0 // m/s, just under 130 km/h
	cyclingDriftRadius = 12.0
	cyclingMaxGap      = 15
)

func cyclingPosition(s StreamSample) (GeoPoint, bool) {
	if s.Latitude == nil || s.Longitude == nil {
		return GeoPoint{}, false
	}
	lat, lon := *s.Latitude, *s.Longitude
	return GeoPoint{Lat: lat, Lon: lon}, lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 && (lat != 0 || lon != 0)
}

// ValidateCyclingStream produces a read-only view of a cycling recording.
// Outdoor rides require a continuous GPS fix. Indoor rides can use their
// odometer or trainer speed. Sensor-only rides still have a valid timeline.
func ValidateCyclingStream(samples []StreamSample) CyclingStream {
	out := CyclingStream{Legs: make([]CyclingLeg, len(samples))}
	withGPS := false
	for _, s := range samples {
		_, position := cyclingPosition(s)
		withGPS = withGPS || s.Latitude != nil || s.Longitude != nil
		out.HasMotion = out.HasMotion || position || s.DistanceM != nil || s.SpeedMps != nil
	}
	maxGap := cyclingGapLimit(samples)
	for i := 1; i < len(samples); i++ {
		out.Legs[i] = cyclingLeg(samples[i-1], samples[i], maxGap, withGPS, out.HasMotion)
	}
	out.removeSpikes(samples)
	if withGPS {
		out.removeDrift(samples)
	}
	return out
}

// A gap of several missing fixes breaks a dense recording. Allow more time
// for Garmin smart recording and other consistently sparse streams.
func cyclingGapLimit(samples []StreamSample) float64 {
	var intervals [cyclingMaxGap]int
	count := 0
	for i := 1; i < len(samples); i++ {
		dt := samples[i].Timestamp.Sub(samples[i-1].Timestamp).Seconds()
		if dt > 0 && dt <= cyclingMaxGap {
			intervals[int(math.Ceil(dt))-1]++
			count++
		}
	}
	if count >= 5 {
		for i, n := range intervals {
			count -= 2 * n
			if count <= 0 {
				return float64(min(cyclingMaxGap, max(5, 3*(i+1))))
			}
		}
	}
	return cyclingMaxGap
}

func cyclingLeg(a, b StreamSample, maxGap float64, withGPS, withMotion bool) CyclingLeg {
	dt := b.Timestamp.Sub(a.Timestamp).Seconds()
	if a.Timestamp.IsZero() || b.Timestamp.IsZero() || dt <= 0 || dt > maxGap ||
		(a.RecordingIndex != 0 && b.RecordingIndex != a.RecordingIndex+1) {
		return CyclingLeg{}
	}

	leg := CyclingLeg{Seconds: dt, Valid: true}
	if !withMotion && !withGPS {
		return leg
	}
	var gps float64
	if withGPS {
		pa, okA := cyclingPosition(a)
		pb, okB := cyclingPosition(b)
		if !okA || !okB {
			return CyclingLeg{}
		}
		gps = HaversineMeters(pa, pb)
		if !validCyclingSpeed(gps / dt) {
			return CyclingLeg{}
		}
		leg.HasGPS = true
	}

	leg.HasMotion = true
	leg.DistanceM = gps
	if a.DistanceM != nil && b.DistanceM != nil {
		delta := *b.DistanceM - *a.DistanceM
		if !(*a.DistanceM >= 0 && *b.DistanceM >= 0) || !validCyclingSpeed(delta/dt) {
			return CyclingLeg{}
		}
		// A frozen odometer can fall back to position. An excessive
		// increment against measured displacement is a discontinuity.
		if withGPS && delta > gps*1.5+5 {
			return CyclingLeg{}
		}
		if !withGPS || (delta >= gps*0.65 && delta <= gps*1.35) {
			leg.DistanceM = delta
		}
	} else if !withGPS {
		if a.SpeedMps == nil || b.SpeedMps == nil || !validCyclingSpeed(*a.SpeedMps) || !validCyclingSpeed(*b.SpeedMps) ||
			!plausibleCyclingChange(*a.SpeedMps, *b.SpeedMps, dt) {
			return CyclingLeg{}
		}
		leg.DistanceM = (*a.SpeedMps + *b.SpeedMps) * dt / 2
	}

	// Low device speed corroborates a stop when position also stays close.
	if withGPS && gps <= cyclingDriftRadius &&
		(gps/dt < 0.55 || (a.SpeedMps != nil && b.SpeedMps != nil &&
			*a.SpeedMps >= 0 && *a.SpeedMps < 0.8 && *b.SpeedMps >= 0 && *b.SpeedMps < 0.8)) {
		leg.DistanceM = 0
	}
	return leg
}

func validCyclingSpeed(speed float64) bool {
	return speed >= 0 && speed <= maxCyclingSpeed
}

func plausibleCyclingChange(before, after, seconds float64) bool {
	change := after - before
	// Compare interval averages at their midpoints. Allow more braking
	// than acceleration and some short-interval GPS noise.
	return change <= 4*seconds+3 && change >= -8*seconds-3
}

func (s CyclingStream) removeSpikes(samples []StreamSample) {
	var rejectPrevious bool
	for i := 2; i < len(s.Legs); i++ {
		before, after := s.Legs[i-1], s.Legs[i]
		rejectNext := false
		if before.Valid && after.Valid && before.HasMotion && after.HasMotion {
			v0, v1 := before.DistanceM/before.Seconds, after.DistanceM/after.Seconds
			if !plausibleCyclingChange(v0, v1, (before.Seconds+after.Seconds)/2) {
				if v1 > v0 {
					rejectNext = true
				} else {
					rejectPrevious = true
				}
			}
			// A short high-speed excursion that snaps back is a spike
			// even if its two scalar speeds agree.
			if before.HasGPS && after.HasGPS && before.Seconds+after.Seconds <= 5 && min(v0, v1) > 12 {
				a, _ := cyclingPosition(samples[i-2])
				b, _ := cyclingPosition(samples[i])
				if HaversineMeters(a, b) < 0.35*(before.DistanceM+after.DistanceM) {
					rejectPrevious, rejectNext = true, true
				}
			}
		}
		if rejectPrevious {
			s.Legs[i-1] = CyclingLeg{}
		}
		rejectPrevious = rejectNext
	}
	if rejectPrevious {
		s.Legs[len(s.Legs)-1] = CyclingLeg{}
	}
}

func (s CyclingStream) removeDrift(samples []StreamSample) {
	for start := 0; start+1 < len(samples); {
		anchor, _ := cyclingPosition(samples[start])
		end, path, displacement := start, 0.0, 0.0
		for end+1 < len(samples) && s.Legs[end+1].HasGPS {
			point, _ := cyclingPosition(samples[end+1])
			net := HaversineMeters(anchor, point)
			if net > cyclingDriftRadius {
				break
			}
			end++
			path += s.Legs[end].DistanceM
			displacement = net
		}
		if samples[end].Timestamp.Sub(samples[start].Timestamp) >= 10*time.Second && path > 2*displacement {
			for i := start + 1; i <= end; i++ {
				s.Legs[i].DistanceM = 0
			}
		}
		start = max(start+1, end)
	}
}

// Sections visits contiguous sample ranges [start,end) without crossing
// a rejected interval. Isolated samples cannot contribute an effort.
func (s CyclingStream) Sections(visit func(start, end int)) {
	for start := 0; start < len(s.Legs); {
		end := start + 1
		for end < len(s.Legs) && s.Legs[end].Valid {
			end++
		}
		if end-start > 1 {
			visit(start, end)
		}
		start = end
	}
}

// SpeedSummary returns distance-weighted movement as an average over moving
// time and the fastest valid interval. A complete stop contributes zero.
func (s CyclingStream) SpeedSummary() (distanceM, movingS, maxSpeed float64) {
	for _, leg := range s.Legs {
		if leg.DistanceM > 0 {
			distanceM += leg.DistanceM
			movingS += leg.Seconds
			maxSpeed = max(maxSpeed, leg.DistanceM/leg.Seconds)
		}
	}
	return distanceM, movingS, maxSpeed
}
