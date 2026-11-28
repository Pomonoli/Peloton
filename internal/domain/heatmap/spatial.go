// Package heatmap aggregates passages over GPS tracks on a fixed projected grid.
package heatmap

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

const (
	WorldHalf   = 20037508.342789244
	TileCount   = 1024
	MaxLatitude = 85.05112878
)

// Level controls the display resolution. The finest grid separates parallel
// tracks by cross-track position and groups opposite travel directions.
type Level int

const (
	Overview Level = iota
	Regional
	Detail
)

var grid = [...]struct{ along, across float64 }{{1500, 750}, {200, 100}, {36, 10}}

type Point struct{ X, Y float64 }

func Project(lon, lat float64) Point {
	return Point{X: lon * WorldHalf / 180, Y: math.Asinh(math.Tan(lat*math.Pi/180)) * WorldHalf / math.Pi}
}

func (p Point) Location() domain.GeoPoint {
	return domain.GeoPoint{Lon: p.X * 180 / WorldHalf, Lat: math.Atan(math.Sinh(p.Y*math.Pi/WorldHalf)) * 180 / math.Pi}
}

type Tile struct{ X, Y int }

func TileAt(p Point) Tile {
	index := func(v float64) int {
		return min(TileCount-1, max(0, int((v+WorldHalf)/(2*WorldHalf)*TileCount)))
	}
	return Tile{index(p.X), index(p.Y)}
}

type Key struct {
	Along, Across int32
	Axis          uint8
}

var directions = [...]Point{{1, 0}, {math.Sqrt2 / 2, math.Sqrt2 / 2}, {0, 1}, {-math.Sqrt2 / 2, math.Sqrt2 / 2}}

// Valid bounds externally supplied keys before they are used to locate a cell.
func (k Key) Valid(level Level) bool {
	if level < Overview || level > Detail || int(k.Axis) >= len(directions) {
		return false
	}
	center := k.center(level)
	margin := grid[level].along + grid[level].across
	return math.Abs(center.X) <= WorldHalf+margin && math.Abs(center.Y) <= WorldHalf+margin
}

func (k Key) center(level Level) Point {
	size, axis := grid[level], directions[k.Axis]
	u, v := (float64(k.Along)+0.5)*size.along, (float64(k.Across)+0.5)*size.across
	return Point{u*axis.X - v*axis.Y, u*axis.Y + v*axis.X}
}

func (k Key) Tile(level Level) Tile {
	center := k.center(level)
	center.X = math.Mod(center.X+3*WorldHalf, 2*WorldHalf) - WorldHalf
	return TileAt(center)
}

// IntersectsPrivacyZone conservatively masks the whole cell if it could
// contain a private point. The projected half diagonal bounds the ground
// distance from the center to any point in the cell.
func (k Key) IntersectsPrivacyZone(level Level, zones []domain.PrivacyZone) bool {
	if len(zones) == 0 {
		return false
	}
	size := grid[level]
	center := k.center(level).Location()
	// Allow for coordinate rounding in the map response as well.
	margin := math.Hypot(size.along, size.across)/2 + 1
	for _, zone := range zones {
		zone.RadiusM += margin
		if zone.Contains(center.Lat, center.Lon) {
			return true
		}
	}
	return false
}

type Weighted struct {
	Sum, Weight float64
}

func (s *Weighted) add(value, weight float64) {
	s.Sum += value * weight
	s.Weight += weight
}

func (s *Weighted) merge(o Weighted) {
	s.Sum += o.Sum
	s.Weight += o.Weight
}

func (s Weighted) Value() *float64 {
	if s.Weight <= 0 {
		return nil
	}
	return new(s.Sum / s.Weight)
}

// Bin stores additive totals so combined maps never average precomputed
// ride averages. U/V are geometry in the direction-aligned grid.
type Bin struct {
	Key                                            Key
	Passages                                       uint32
	Distance, Moving                               float64
	HeartRate, Power, Elevation, Gradient, Cadence Weighted
	GeometryV                                      float64
	MinU, MaxU                                     float64
}

func (b *Bin) Merge(other Bin) {
	if b.Moving == 0 {
		*b = other
		return
	}
	b.Passages += other.Passages
	b.Distance += other.Distance
	b.Moving += other.Moving
	b.HeartRate.merge(other.HeartRate)
	b.Power.merge(other.Power)
	b.Cadence.merge(other.Cadence)
	b.Elevation.merge(other.Elevation)
	b.Gradient.merge(other.Gradient)
	b.GeometryV += other.GeometryV
	b.MinU, b.MaxU = min(b.MinU, other.MinU), max(b.MaxU, other.MaxU)
}

func (b Bin) Line() [2]domain.GeoPoint {
	axis := directions[b.Key.Axis]
	cross := b.GeometryV / b.Distance
	project := func(along float64) domain.GeoPoint {
		return (Point{along*axis.X - cross*axis.Y, along*axis.Y + cross*axis.X}).Location()
	}
	return [2]domain.GeoPoint{project(b.MinU), project(b.MaxU)}
}

func Ordered(bins map[Key]*Bin) []Bin {
	out := make([]Bin, 0, len(bins))
	for _, b := range bins {
		out = append(out, *b)
	}
	slices.SortFunc(out, func(a, b Bin) int {
		if c := cmp.Compare(a.Key.Axis, b.Key.Axis); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Key.Along, b.Key.Along); c != 0 {
			return c
		}
		return cmp.Compare(a.Key.Across, b.Key.Across)
	})
	return out
}

type reading struct {
	point     domain.GeoPoint
	projected Point
	altitude  *float64
	valid     bool
}

// Aggregate converts a canonical track into cumulative contributions at all
// display levels. Each leg is divided exactly at grid boundaries.
func Aggregate(samples []domain.StreamSample) [3][]Bin {
	var out [3][]Bin
	readings := make([]reading, len(samples))
	for i, s := range samples {
		if !within(s.Latitude, -MaxLatitude, MaxLatitude) || !within(s.Longitude, -180, 180) ||
			(*s.Latitude == 0 && *s.Longitude == 0) {
			continue
		}
		readings[i] = reading{point: domain.GeoPoint{Lat: *s.Latitude, Lon: *s.Longitude}, projected: Project(*s.Longitude, *s.Latitude),
			altitude: smoothAltitude(samples, i), valid: true}
	}
	validated := domain.ValidateCyclingStream(samples)
	smoothGPS(readings, samples, validated.Legs)
	legs := make([]float64, len(samples))
	for i := 1; i < len(samples); i++ {
		if readings[i-1].valid && readings[i].valid {
			legs[i] = validated.Legs[i].DistanceM
		}
	}
	var levels [3]map[Key]*Bin
	var visits [3]map[Key]visit
	for i := range levels {
		levels[i], visits[i] = make(map[Key]*Bin), make(map[Key]visit)
	}
	var traveled float64
	for i := 1; i < len(samples); i++ {
		a, b := samples[i-1], samples[i]
		pa, pb := readings[i-1], readings[i]
		legTime := validated.Legs[i]
		distance := legs[i]
		if distance == 0 {
			if !legTime.Valid {
				for _, previous := range visits {
					clear(previous)
				}
			}
			continue
		}
		dx, dy := pb.projected.X-pa.projected.X, pb.projected.Y-pa.projected.Y
		// Split the world copy at the antimeridian before walking the grid.
		if math.Abs(dx) > WorldHalf {
			dx -= math.Copysign(2*WorldHalf, dx)
		}
		axisIndex := direction(dx, dy)
		axis := directions[axisIndex]
		altitude := mean(pa.altitude, pb.altitude)
		gradient := slope(readings, legs, i)
		if gradient == nil && a.Grade != nil && b.Grade != nil {
			gradient = mean(smoothGrade(samples, i-1), smoothGrade(samples, i))
		}
		leg := contribution{distance: distance, time: legTime.Seconds, hr: meanInt(a.HeartRateBpm, b.HeartRateBpm, 25, 250),
			power: meanInt(a.PowerW, b.PowerW, 0, 3000), cadence: meanInt(a.Cadence, b.Cadence, 0, 255),
			altitude: altitude, gradient: gradient, traveled: traveled}
		for level := range levels {
			bins, previous := levels[level], visits[level]
			rasterize(pa.projected, dx, dy, axis, uint8(axisIndex), Level(level), func(key Key, u0, u1, cross, from, to float64) {
				bin := bins[key]
				if bin == nil {
					bin = &Bin{Key: key, MinU: math.Inf(1), MaxU: math.Inf(-1)}
					bins[key] = bin
				}
				bin.record(previous, leg, u0, u1, cross, from, to)
			})
		}
		traveled += distance
	}
	for level := range levels {
		out[level] = Ordered(levels[level])
	}
	return out
}

// Blend only small cross-track deviations toward the neighboring points.
// Large bends, gaps and differences between distinct roads are retained.
func smoothGPS(readings []reading, samples []domain.StreamSample, legs []domain.CyclingLeg) {
	smoothed := make([]Point, len(readings))
	for i := 1; i+1 < len(readings); i++ {
		a, b, c := readings[i-1], readings[i], readings[i+1]
		smoothed[i] = b.projected
		dt := samples[i+1].Timestamp.Sub(samples[i-1].Timestamp)
		if !a.valid || !b.valid || !c.valid || !legs[i].Valid || !legs[i+1].Valid || dt > 12*time.Second {
			continue
		}
		dx, dy := c.projected.X-a.projected.X, c.projected.Y-a.projected.Y
		scale := math.Cos(b.point.Lat * math.Pi / 180)
		chord := dx*dx + dy*dy
		if chord == 0 || math.Abs(dx) > WorldHalf || math.Sqrt(chord)*scale < 4 {
			continue
		}
		fraction := ((b.projected.X-a.projected.X)*dx + (b.projected.Y-a.projected.Y)*dy) / chord
		if fraction <= 0 || fraction >= 1 {
			continue
		}
		x, y := a.projected.X+fraction*dx-b.projected.X, a.projected.Y+fraction*dy-b.projected.Y
		if math.Hypot(x, y)*scale <= 6 {
			smoothed[i] = Point{b.projected.X + x*0.6, b.projected.Y + y*0.6}
		}
	}
	for i := 1; i+1 < len(readings); i++ {
		readings[i].projected = smoothed[i]
	}
}

func within(v *float64, lower, upper float64) bool { return v != nil && *v >= lower && *v <= upper }

func direction(dx, dy float64) int {
	a := int(math.Round(math.Atan2(dy, dx) * 4 / math.Pi))
	return (a + 4) % 4
}

func mean(a, b *float64) *float64 {
	if a == nil || b == nil {
		return nil
	}
	return new((*a + *b) / 2)
}

func meanInt(a, b *int16, minimum, maximum int16) *float64 {
	if a == nil || b == nil || *a < minimum || *a > maximum || *b < minimum || *b > maximum {
		return nil
	}
	return new((float64(*a) + float64(*b)) / 2)
}

func smoothAltitude(samples []domain.StreamSample, i int) *float64 {
	if !within(samples[i].AltitudeM, -500, 9000) {
		return nil
	}
	var values [5]float64
	n := 0
	for _, sample := range samples[max(0, i-2):min(len(samples), i+3)] {
		if within(sample.AltitudeM, -500, 9000) && absDuration(sample.Timestamp.Sub(samples[i].Timestamp)) <= 10*time.Second {
			values[n] = *sample.AltitudeM
			n++
		}
	}
	slices.Sort(values[:n])
	return new((values[(n-1)/2] + values[n/2]) / 2)
}

func absDuration(t time.Duration) time.Duration {
	if t < 0 {
		return -t
	}
	return t
}

func smoothGrade(samples []domain.StreamSample, i int) *float64 {
	if g := *samples[i].Grade; !(g >= -35 && g <= 35) {
		return nil
	}
	var values [5]float64
	n := 0
	for _, sample := range samples[max(0, i-2):min(len(samples), i+3)] {
		if g := sample.Grade; g != nil && *g >= -35 && *g <= 35 && absDuration(sample.Timestamp.Sub(samples[i].Timestamp)) <= 10*time.Second {
			values[n] = float64(*g)
			n++
		}
	}
	if n == 0 {
		return nil
	}
	slices.Sort(values[:n])
	return new((values[(n-1)/2] + values[n/2]) / 2)
}

func slope(points []reading, legs []float64, i int) *float64 {
	if points[i-1].altitude == nil || points[i].altitude == nil {
		return nil
	}
	lo, hi := i-1, i
	distance := legs[i]
	for distance < 35 && (lo > 0 || hi < len(points)-1) {
		var extended bool
		if lo > 0 {
			if d := legs[lo]; d > 0 && points[lo-1].altitude != nil {
				lo--
				distance += d
				extended = true
			}
		}
		if hi < len(points)-1 {
			if d := legs[hi+1]; d > 0 && points[hi+1].altitude != nil {
				hi++
				distance += d
				extended = true
			}
		}
		if !extended {
			break
		}
	}
	if distance < 12 {
		return nil
	}
	grade := 100 * (*points[hi].altitude - *points[lo].altitude) / distance
	if math.Abs(grade) > 35 {
		return nil
	}
	return &grade
}

type contribution struct {
	distance, time                         float64
	hr, power, altitude, gradient, cadence *float64
	traveled                               float64
}
type visit struct {
	end, direction, reversal float64
}

func (b *Bin) record(visits map[Key]visit, leg contribution, u0, u1, cross, from, to float64) {
	d, t := leg.distance*(to-from), leg.time*(to-from)
	if d <= 0 || t <= 0 {
		return
	}
	previous, seen := visits[b.Key]
	offset := leg.traveled + leg.distance*from
	heading := math.Copysign(1, u1-u0)
	// Returning through a cell after leaving it counts again. Small
	// oscillations at a grid boundary do not create extra passages.
	gap := offset - previous.end
	if !seen || gap >= 15 || (previous.direction != heading && gap >= 4) {
		b.Passages++
		previous.direction, previous.reversal = heading, 0
	} else if previous.direction != heading {
		previous.reversal += d
		if previous.reversal >= 8 {
			b.Passages++
			previous.direction, previous.reversal = heading, 0
		}
	} else {
		previous.reversal = 0
	}
	previous.end = leg.traveled + leg.distance*to
	visits[b.Key] = previous
	b.Distance += d
	b.Moving += t
	b.GeometryV += cross * d
	b.MinU = min(b.MinU, u0, u1)
	b.MaxU = max(b.MaxU, u0, u1)
	if leg.hr != nil {
		b.HeartRate.add(*leg.hr, t)
	}
	if leg.power != nil {
		b.Power.add(*leg.power, t)
	}
	if leg.cadence != nil {
		b.Cadence.add(*leg.cadence, t)
	}
	if leg.altitude != nil {
		b.Elevation.add(*leg.altitude, t)
	}
	if leg.gradient != nil {
		b.Gradient.add(*leg.gradient*heading, d)
	}
}
