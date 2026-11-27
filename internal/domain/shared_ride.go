package domain

import (
	"math"
	"time"
)

const (
	sharedRadiusM    = 50
	encounterRadiusM = 30
	sharedMinimum    = 5 * time.Minute
	sharedGap        = 30 * time.Second
	encounterGap     = 12 * time.Second
)

type SharedMotion struct {
	Start, End time.Time
	DistanceM  float64
	Seconds    float64
}

type RideEncounter struct {
	Time     time.Time
	Location GeoPoint
}

type RideProximity struct {
	Shared     *SharedMotion
	Encounters []RideEncounter
}

type movingGPSLeg struct {
	start, end  time.Time
	from, delta routeVector
	speed       float64
}

// CompareRideTracks compares continuous moving intervals on the absolute
// recording clock. It interpolates only inside validated GPS legs.
func CompareRideTracks(first, second []StreamSample, firstZones, secondZones []PrivacyZone) RideProximity {
	a := movingGPSLegs(first, firstZones)
	b := movingGPSLegs(second, secondZones)
	var shared, encounters []proximityInterval
	for i, j := 0, 0; i < len(a) && j < len(b); {
		left, right := a[i], b[j]
		match := compareGPSLegs(left, right)
		if match.shared != nil {
			shared = append(shared, *match.shared)
		}
		if match.close != nil {
			encounters = append(encounters, *match.close)
		}
		if !left.end.After(right.end) {
			i++
		}
		if !right.end.After(left.end) {
			j++
		}
	}
	if ride := sharedMotion(shared); ride != nil {
		return RideProximity{Shared: ride}
	}
	return RideProximity{Encounters: briefEncounters(encounters)}
}

func movingGPSLegs(samples []StreamSample, zones []PrivacyZone) []movingGPSLeg {
	var out []movingGPSLeg
	validated := ValidateCyclingStream(samples)
	for i := 1; i < len(samples); i++ {
		leg := validated.Legs[i]
		if !leg.Valid || !leg.HasGPS || leg.DistanceM < 0.8*leg.Seconds {
			continue
		}
		a, _ := cyclingPosition(samples[i-1])
		b, _ := cyclingPosition(samples[i])
		from := routePosition(a)
		delta := routePosition(b).sub(from)
		if delta.dot(delta) < .64*leg.Seconds*leg.Seconds {
			continue
		}
		if !visibleMovingLeg(from, delta, zones) {
			continue
		}
		out = append(out, movingGPSLeg{samples[i-1].Timestamp, samples[i].Timestamp, from, delta, leg.DistanceM / leg.Seconds})
	}
	return out
}

func visibleMovingLeg(from, delta routeVector, zones []PrivacyZone) bool {
	lengthSquared := delta.dot(delta)
	for _, zone := range zones {
		offset := routePosition(GeoPoint{Lat: zone.Lat, Lon: zone.Lng}).sub(from)
		nearest := offset.sub(delta.scale(max(0, min(1, offset.dot(delta)/lengthSquared))))
		// The reported encounter location lies between the two tracks.
		// Leave enough room that its midpoint also stays outside the zone.
		radius := zone.RadiusM + encounterRadiusM/2
		if nearest.dot(nearest) <= radius*radius {
			return false
		}
	}
	return true
}

func (v routeVector) add(w routeVector) routeVector {
	return routeVector{v.x + w.x, v.y + w.y, v.z + w.z}
}

func (l movingGPSLeg) at(t time.Time) routeVector {
	return l.from.add(l.delta.scale(float64(t.Sub(l.start)) / float64(l.end.Sub(l.start))))
}

type proximityInterval struct {
	start, end time.Time
	closest    time.Time
	location   routeVector
	separation float64
	distanceM  float64
}

type legProximity struct {
	shared, close *proximityInterval
}

func compareGPSLegs(a, b movingGPSLeg) legProximity {
	start, end := a.start, a.end
	if start.Before(b.start) {
		start = b.start
	}
	if end.After(b.end) {
		end = b.end
	}
	if !end.After(start) {
		return legProximity{}
	}
	left, right := a.at(start), b.at(start)
	offset := left.sub(right)
	delta := a.at(end).sub(b.at(end)).sub(offset)
	// The relative path is linear between fixes. A cheap sphere bound
	// rejects distant legs before the exact proximity calculation.
	if offset.dot(offset) > sharedRadiusM*sharedRadiusM &&
		math.Sqrt(offset.dot(offset)) > sharedRadiusM+math.Sqrt(delta.dot(delta)) {
		return legProximity{}
	}
	midpoint := left.add(right).scale(.5)
	centerDelta := a.at(end).add(b.at(end)).scale(.5).sub(midpoint)
	squared := delta.dot(delta)
	var projection, closest float64
	if squared > 1e-12 {
		projection = -offset.dot(delta) / squared
		closest = max(0, min(1, projection))
	}
	separation := offset.add(delta.scale(closest))
	minimum := separation.dot(separation)
	span := end.Sub(start)
	at := func(fraction float64) time.Time {
		return start.Add(time.Duration(float64(span) * fraction))
	}
	within := func(radius float64) *proximityInterval {
		if minimum >= radius*radius {
			return nil
		}
		lo, hi := 0.0, 1.0
		if squared > 1e-12 {
			nearest := offset.add(delta.scale(projection))
			r := math.Sqrt(max(0, radius*radius-nearest.dot(nearest)) / squared)
			lo, hi = max(0, projection-r), min(1, projection+r)
		}
		if hi <= lo {
			return nil
		}
		return &proximityInterval{
			start: at(lo), end: at(hi), closest: at(closest), separation: minimum,
			location:  midpoint.add(centerDelta.scale(closest)),
			distanceM: (hi - lo) * span.Seconds() * (a.speed + b.speed) / 2,
		}
	}
	out := legProximity{close: within(encounterRadiusM)}
	// cos(60°) = 0.5. Compare the observed direction of each recording.
	if a.delta.dot(b.delta) >= .5*math.Sqrt(a.delta.dot(a.delta)*b.delta.dot(b.delta)) {
		out.shared = within(sharedRadiusM)
	}
	return out
}

func sharedMotion(intervals []proximityInterval) *SharedMotion {
	var bouts []SharedMotion
	for i, interval := range intervals {
		if i == 0 || interval.start.Sub(intervals[i-1].end) > sharedGap {
			bouts = append(bouts, SharedMotion{Start: interval.start})
		}
		bout := &bouts[len(bouts)-1]
		bout.End = interval.end
		bout.Seconds += interval.end.Sub(interval.start).Seconds()
		bout.DistanceM += interval.distanceM
	}
	confirmed := false
	for _, bout := range bouts {
		if bout.Seconds >= sharedMinimum.Seconds()-1e-6 && bout.Seconds >= .7*bout.End.Sub(bout.Start).Seconds() {
			confirmed = true
			break
		}
	}
	if !confirmed {
		return nil
	}
	var ride SharedMotion
	for _, bout := range bouts {
		// Once confirmed, include sustained sections after longer separations.
		// Brief incidental proximity, time apart and stops add no distance.
		if bout.Seconds < 30 || bout.Seconds < .7*bout.End.Sub(bout.Start).Seconds() {
			continue
		}
		if ride.Start.IsZero() {
			ride.Start = bout.Start
		}
		ride.End = bout.End
		ride.Seconds += bout.Seconds
		ride.DistanceM += bout.DistanceM
	}
	return &ride
}

func briefEncounters(intervals []proximityInterval) []RideEncounter {
	var out []RideEncounter
	for start := 0; start < len(intervals); {
		best, moving := intervals[start], intervals[start].end.Sub(intervals[start].start)
		end := start + 1
		for end < len(intervals) && intervals[end].start.Sub(intervals[end-1].end) <= encounterGap {
			next := intervals[end]
			moving += next.end.Sub(next.start)
			if next.separation < best.separation {
				best = next
			}
			end++
		}
		// Require corroborated movement and a brief episode, even if noise
		// or a pause temporarily interrupted close proximity.
		if moving >= time.Second && intervals[end-1].end.Sub(intervals[start].start) <= 90*time.Second {
			p := best.location
			out = append(out, RideEncounter{best.closest, GeoPoint{
				Lat: math.Atan2(p.z, math.Hypot(p.x, p.y)) * 180 / math.Pi,
				Lon: math.Atan2(p.y, p.x) * 180 / math.Pi,
			}})
		}
		start = end
	}
	return out
}
