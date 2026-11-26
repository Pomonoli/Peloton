package domain

import "math"

const (
	routeToleranceM = 35
	routeSampleM    = 15
	MinRouteOverlap = .80
)

type routeVector struct{ x, y, z float64 }

func (v routeVector) sub(w routeVector) routeVector {
	return routeVector{v.x - w.x, v.y - w.y, v.z - w.z}
}

func (v routeVector) dot(w routeVector) float64 {
	return v.x*w.x + v.y*w.y + v.z*w.z
}

func (v routeVector) scale(f float64) routeVector {
	return routeVector{v.x * f, v.y * f, v.z * f}
}

func routePosition(p GeoPoint) routeVector {
	const earth = 6_371_000
	sinLat, cosLat := math.Sincos(p.Lat * math.Pi / 180)
	sinLon, cosLon := math.Sincos(p.Lon * math.Pi / 180)
	return routeVector{earth * cosLat * cosLon, earth * cosLat * sinLon, earth * sinLat}
}

type routeCell struct{ x, y, z int }

type routePart struct {
	at, direction routeVector
	length        float64
}

// RouteCoverage indexes short, length-weighted GPS intervals. The 3D grid
// works across the antimeridian and stays in metres at any latitude.
type RouteCoverage struct {
	grid   map[routeCell][]routePart
	length float64
	size   int
}

func routeCellAt(at routeVector) routeCell {
	return routeCell{int(math.Floor(at.x / routeToleranceM)), int(math.Floor(at.y / routeToleranceM)), int(math.Floor(at.z / routeToleranceM))}
}

func NewRouteCoverage(samples []StreamSample, activityType ActivityType) RouteCoverage {
	out := newRouteCoverage()
	var cycling CyclingStream
	if activityType == ActivityTypeRide {
		cycling = ValidateCyclingStream(samples)
	}
	for i := 1; i < len(samples); i++ {
		a, goodA := cyclingPosition(samples[i-1])
		b, goodB := cyclingPosition(samples[i])
		dt := samples[i].Timestamp.Sub(samples[i-1].Timestamp).Seconds()
		if !goodA || !goodB || dt <= 0 || dt > 30 {
			continue
		}
		if activityType == ActivityTypeRide && !(cycling.Legs[i].Valid && cycling.Legs[i].HasGPS && cycling.Legs[i].DistanceM > 0) {
			continue
		}
		start, end := routePosition(a), routePosition(b)
		vector := end.sub(start)
		length := math.Sqrt(vector.dot(vector))
		if length < 1 || length/dt > CyclingMaxSpeedMps {
			continue
		}
		out.add(start, vector, length)
	}
	return out
}

func newRouteCoverage() RouteCoverage {
	return RouteCoverage{grid: make(map[routeCell][]routePart)}
}

func (r *RouteCoverage) add(start, vector routeVector, length float64) {
	n := int(math.Ceil(length / routeSampleM))
	direction := vector.scale(1 / length)
	for j := range n {
		offset := vector.scale((float64(j) + .5) / float64(n))
		at := routeVector{start.x + offset.x, start.y + offset.y, start.z + offset.z}
		cell := routeCellAt(at)
		r.grid[cell] = append(r.grid[cell], routePart{at, direction, length / float64(n)})
	}
	r.length += length
	r.size += n
}

// Size is the number of indexed route intervals.
func (r RouteCoverage) Size() int { return r.size }

// Overlap is the lesser directional coverage, so a short shared section or a
// route fully contained in a substantially longer route cannot qualify.
func (r RouteCoverage) Overlap(other RouteCoverage) float64 {
	if r.length == 0 || other.length == 0 ||
		min(r.length, other.length) < MinRouteOverlap*max(r.length, other.length) {
		return 0
	}
	forward := r.coveredBy(other, 1)
	if forward < MinRouteOverlap {
		return forward
	}
	return min(forward, other.coveredBy(r, 1))
}

func (r RouteCoverage) coveredBy(other RouteCoverage, direction float64) float64 {
	var covered float64
	for cell, parts := range r.grid {
		for _, part := range parts {
			if other.covers(cell, part, direction) {
				covered += part.length
			}
		}
	}
	return min(1, covered/r.length)
}

var routeNeighborOffsets = func() [27]routeCell {
	var offsets [27]routeCell
	i := 0
	for x := -1; x <= 1; x++ {
		for y := -1; y <= 1; y++ {
			for z := -1; z <= 1; z++ {
				offsets[i] = routeCell{x, y, z}
				i++
			}
		}
	}
	return offsets
}()

func (r RouteCoverage) covers(cell routeCell, part routePart, direction float64) bool {
	for _, offset := range routeNeighborOffsets {
		key := routeCell{cell.x + offset.x, cell.y + offset.y, cell.z + offset.z}
		for _, candidate := range r.grid[key] {
			delta := candidate.at.sub(part.at)
			if delta.dot(delta) <= routeToleranceM*routeToleranceM && candidate.direction.dot(part.direction)*direction >= .5 {
				return true
			}
		}
	}
	return false
}
