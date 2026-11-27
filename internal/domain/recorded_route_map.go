package domain

import (
	"context"
	"math"
	"slices"
)

const (
	routeMapPieceM     = 24
	routeMapToleranceM = 6
	routeMapDirection  = .965925826 // 15 degrees, in either direction
)

// RecordedRouteMapLine identifies the unique catalog routes present on a
// connected line. Route indexes refer to the ordered input catalog.
type RecordedRouteMapLine struct {
	Points []GeoPoint
	Routes []int
}

type routeMapPiece struct {
	start, end     GeoPoint
	vector, center routeVector
	length         float64
	route          int
	corner         bool
}

func RecordedRouteMap(ctx context.Context, tracks []RecordedTrack) ([]RecordedRouteMapLine, error) {
	var lines [][]routeMapPiece
	grid := make(map[routeCell][]*routeMapPiece)
	for route, track := range tracks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, line := range track {
			pieces := splitRouteMapLine(line, route)
			for i := range pieces {
				piece := &pieces[i]
				cell := routeCellAt(piece.center)
				grid[cell] = append(grid[cell], piece)
			}
			lines = append(lines, pieces)
		}
	}

	var out []RecordedRouteMapLine
	seen := make([]int, len(tracks))
	generation := 0
	var matches []int
	for _, line := range lines {
		var current RecordedRouteMapLine
		for i := range line {
			if i%1024 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			piece := &line[i]
			generation++
			seen[piece.route] = generation
			matches = append(matches[:0], piece.route)
			cell := routeCellAt(piece.center)
			for _, offset := range routeNeighborOffsets {
				for _, other := range grid[routeCell{cell.x + offset.x, cell.y + offset.y, cell.z + offset.z}] {
					if seen[other.route] == generation || !piece.overlaps(other) {
						continue
					}
					seen[other.route] = generation
					matches = append(matches, other.route)
				}
			}
			slices.Sort(matches)
			if !slices.Equal(current.Routes, matches) {
				if len(current.Points) > 0 {
					if current.Points[len(current.Points)-1] != piece.start {
						current.Points = append(current.Points, piece.start)
					}
					out = append(out, current)
				}
				current = RecordedRouteMapLine{Points: []GeoPoint{piece.start}, Routes: slices.Clone(matches)}
			}
			if piece.corner {
				current.Points = append(current.Points, piece.end)
			}
		}
		if len(current.Points) > 0 {
			out = append(out, current)
		}
	}
	return out, nil
}

func (a *routeMapPiece) overlaps(b *routeMapPiece) bool {
	if math.Abs(a.vector.dot(b.vector)) < routeMapDirection {
		return false
	}
	delta := a.center.sub(b.center)
	along := delta.dot(b.vector)
	// Match lateral distance and longitudinal coverage independently.
	// A close endpoint at a junction must also cover the selected section.
	if math.Abs(along) > b.length/2+1 {
		return false
	}
	cross := delta.sub(b.vector.scale(along))
	return cross.dot(cross) <= routeMapToleranceM*routeMapToleranceM
}

// Simplify at two metres before estimating headings so GPS jitter does not
// fragment a road into short pieces at inconsistent angles.
func (track RecordedTrack) MapGeometry() RecordedTrack {
	out := make(RecordedTrack, 0, len(track))
	for _, line := range track {
		if len(line) >= 2 {
			out = append(out, simplifyRouteMapLine(line))
		}
	}
	return out
}

func simplifyRouteMapLine(line []RecordedRoutePoint) []RecordedRoutePoint {
	positions := make([]routeVector, len(line))
	for i, p := range line {
		positions[i] = routePosition(GeoPoint{Lat: p.Lat, Lon: p.Lon})
	}
	keep := make([]bool, len(line))
	type span struct{ start, end int }
	pending := []span{{0, len(line) - 1}}
	keep[0], keep[len(line)-1] = true, true
	for len(pending) > 0 {
		s := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		a := positions[s.start]
		v := positions[s.end].sub(a)
		length2 := v.dot(v)
		farthest, maxDistance2 := 0, 4.0
		for i := s.start + 1; i < s.end; i++ {
			d := positions[i].sub(a)
			if length2 > 0 {
				d = d.sub(v.scale(min(1, max(0, d.dot(v)/length2))))
			}
			if distance2 := d.dot(d); distance2 > maxDistance2 {
				farthest, maxDistance2 = i, distance2
			}
		}
		if farthest > 0 {
			keep[farthest] = true
			pending = append(pending, span{s.start, farthest}, span{farthest, s.end})
		}
	}
	out := make([]RecordedRoutePoint, 0, len(line)/4+2)
	for i, p := range line {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

func splitRouteMapLine(line []RecordedRoutePoint, route int) []routeMapPiece {
	var out []routeMapPiece
	for i := 1; i < len(line); i++ {
		a := GeoPoint{Lat: line[i-1].Lat, Lon: line[i-1].Lon}
		b := GeoPoint{Lat: line[i].Lat, Lon: line[i].Lon}
		start, end := routePosition(a), routePosition(b)
		v := end.sub(start)
		length := math.Sqrt(v.dot(v))
		if length == 0 {
			continue
		}
		direction := v.scale(1 / length)
		steps := int(math.Ceil(length / routeMapPieceM))
		lonDelta := math.Remainder(b.Lon-a.Lon, 360)
		previous := a
		for step := 1; step <= steps; step++ {
			fraction := float64(step) / float64(steps)
			point := GeoPoint{Lon: math.Remainder(a.Lon+lonDelta*fraction, 360), Lat: a.Lat + (b.Lat-a.Lat)*fraction}
			if step == steps {
				point = b
			}
			// The two-point line is kept in its nearest world copy by the map client.
			f := (float64(step) - .5) / float64(steps)
			center := routeVector{start.x + v.x*f, start.y + v.y*f, start.z + v.z*f}
			out = append(out, routeMapPiece{start: previous, end: point, vector: direction,
				center: center, length: length / float64(steps), route: route, corner: step == steps})
			previous = point
		}
	}
	return out
}
