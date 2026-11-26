package domain

import (
	"math"
	"time"

	"github.com/google/uuid"
)

const (
	MinRecordedRouteOverlap     = .80
	MinRecordedRouteLengthRatio = .80
)

type RecordedRouteID uuid.UUID

func (id RecordedRouteID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id RecordedRouteID) String() string  { return id.UUID().String() }

type RecordedRouteKind string

const (
	RecordedLoop         RecordedRouteKind = "loop"
	RecordedOutAndBack   RecordedRouteKind = "out_and_back"
	RecordedPointToPoint RecordedRouteKind = "point_to_point"
)

type RecordedRoutePoint struct {
	Lat, Lon   float64
	ElevationM *float64
	DistanceM  float64
}

type RecordedTrack [][]RecordedRoutePoint

// NewRecordedTrack retains validated recorded positions and separates GPS gaps.
// The distance axis measures visible GPS legs, independently of device odometers.
func NewRecordedTrack(samples []StreamSample) (track RecordedTrack, length float64) {
	validated := ValidateCyclingStream(samples)
	var line []RecordedRoutePoint
	for i := 1; i < len(samples); i++ {
		leg := validated.Legs[i]
		dt := samples[i].Timestamp.Sub(samples[i-1].Timestamp)
		if !leg.Valid || !leg.HasGPS || dt <= 0 || dt > 30*time.Second {
			if len(line) > 1 {
				track = append(track, line)
			}
			line = nil
			continue
		}
		a, _ := cyclingPosition(samples[i-1])
		b, _ := cyclingPosition(samples[i])
		distance := haversineMeters(a.Lat, a.Lon, b.Lat, b.Lon)
		if distance < 1 {
			continue
		}
		if len(line) == 0 {
			line = append(line, recordedPoint(a, samples[i-1], length))
		}
		length += distance
		line = append(line, recordedPoint(b, samples[i], length))
	}
	if len(line) > 1 {
		track = append(track, line)
	}
	if length < 300 {
		return nil, 0
	}
	return track, length
}

func recordedPoint(p GeoPoint, s StreamSample, meters float64) RecordedRoutePoint {
	elevation := s.AltitudeM
	if elevation != nil && (math.IsNaN(*elevation) || math.IsInf(*elevation, 0)) {
		elevation = nil
	}
	return RecordedRoutePoint{Lat: p.Lat, Lon: p.Lon, ElevationM: elevation, DistanceM: meters}
}

func (track RecordedTrack) Coverage() RouteCoverage {
	out := newRouteCoverage()
	for _, line := range track {
		for i := 1; i < len(line); i++ {
			a := routePosition(GeoPoint{Lat: line[i-1].Lat, Lon: line[i-1].Lon})
			b := routePosition(GeoPoint{Lat: line[i].Lat, Lon: line[i].Lon})
			vector := b.sub(a)
			if length := math.Sqrt(vector.dot(vector)); length > 0 {
				out.add(a, vector, length)
			}
		}
	}
	return out
}

// RecordedOverlap uses a consistent travel direction across the whole route.
// Circular tracks can begin anywhere, since coverage is measured spatially.
func (r RouteCoverage) RecordedOverlap(other RouteCoverage) float64 {
	if r.length == 0 || other.length == 0 ||
		min(r.length, other.length) < MinRecordedRouteLengthRatio*max(r.length, other.length) {
		return 0
	}
	// Ignore roundoff from summing intervals in grid iteration order.
	const precision = 1e12
	best := 0.0
	for _, direction := range []float64{1, -1} {
		forward := math.Round(r.coveredBy(other, direction)*precision) / precision
		if forward >= MinRecordedRouteOverlap {
			reverse := math.Round(other.coveredBy(r, direction)*precision) / precision
			best = max(best, min(forward, reverse))
		}
		if best == 1 {
			break
		}
	}
	if best < MinRecordedRouteOverlap {
		return 0
	}
	return best
}

func (track RecordedTrack) Kind(length float64, coverage RouteCoverage) RecordedRouteKind {
	first, last := track[0][0], track[len(track)-1][len(track[len(track)-1])-1]
	if haversineMeters(first.Lat, first.Lon, last.Lat, last.Lon) > min(400, max(80, length*.025)) {
		return RecordedPointToPoint
	}
	if coverage.coveredBy(coverage, -1) >= .8 {
		return RecordedOutAndBack
	}
	return RecordedLoop
}

// Preview samples each connected line without joining GPS gaps.
func (track RecordedTrack) Preview(target int) RecordedTrack {
	size := 0
	for _, line := range track {
		size += len(line)
	}
	step := max(1, (size+target-1)/target)
	var out RecordedTrack
	for _, line := range track {
		preview := make([]RecordedRoutePoint, 0, len(line)/step+2)
		for i := 0; i < len(line)-1; i += step {
			p := line[i]
			p.ElevationM = nil
			preview = append(preview, p)
		}
		last := line[len(line)-1]
		last.ElevationM = nil
		out = append(out, append(preview, last))
	}
	return out
}

// Mask removes private positions and any line crossing a zone. Keeping the
// remaining positions in separate lines prevents drawing a shortcut across it.
func (track RecordedTrack) Mask(zones []PrivacyZone) RecordedTrack {
	if len(zones) == 0 {
		return track
	}
	var out RecordedTrack
	for _, line := range track {
		out = appendVisibleRouteLines(out, line, zones)
	}
	return out
}

func appendVisibleRouteLines(out RecordedTrack, line []RecordedRoutePoint, zones []PrivacyZone) RecordedTrack {
	start := 0
	for i, p := range line {
		masked := PointInAnyZone(zones, p.Lat, p.Lon)
		if masked || (i > start && routeLegCrossesZones(line[i-1], p, zones)) {
			if i-start >= 2 {
				out = append(out, line[start:i])
			}
			start = i
			if masked {
				start++
			}
		}
	}
	if len(line)-start >= 2 {
		out = append(out, line[start:])
	}
	return out
}

func routeLegCrossesZones(a, b RecordedRoutePoint, zones []PrivacyZone) bool {
	start := routePosition(GeoPoint{Lat: a.Lat, Lon: a.Lon})
	line := routePosition(GeoPoint{Lat: b.Lat, Lon: b.Lon}).sub(start)
	lengthSquared := line.dot(line)
	if lengthSquared == 0 {
		return false
	}
	for _, zone := range zones {
		offset := routePosition(GeoPoint{Lat: zone.Lat, Lon: zone.Lng}).sub(start)
		delta := offset.sub(line.scale(max(0, min(1, offset.dot(line)/lengthSquared))))
		if delta.dot(delta) <= zone.RadiusM*zone.RadiusM {
			return true
		}
	}
	return false
}
