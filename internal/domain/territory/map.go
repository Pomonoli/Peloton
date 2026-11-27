package territory

import (
	"math"

	h3 "github.com/dimchansky/h3-go"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// Masked reports whether a cell overlaps a geographic privacy zone. The
// whole contribution is withheld because scoring reveals activity anywhere
// within the cell, even when individual GPS positions are never disclosed.
func Masked(cell Cell, zones []domain.PrivacyZone) (bool, error) {
	if len(zones) == 0 {
		return false, nil
	}
	boundary, err := cell.Boundary()
	if err != nil {
		return false, err
	}
	center, err := cell.LatLng()
	if err != nil {
		return false, err
	}
	for _, zone := range zones {
		point := h3.LatLngDegs(zone.Lat, zone.Lng)
		// Resolution 8 cells fit comfortably within 1 km of their center.
		if h3.GreatCircleDistanceM(center, point) > zone.RadiusM+1000 {
			continue
		}
		containing, err := point.Cell(Resolution)
		if err != nil {
			return false, err
		}
		if cell == containing || zoneIntersectsBoundary(zone, point, boundary) {
			return true, nil
		}
	}
	return false, nil
}

func zoneIntersectsBoundary(zone domain.PrivacyZone, point h3.LatLng, boundary h3.CellBoundary) bool {
	p := unit(point)
	for i := 0; i < boundary.Len(); i++ {
		a, b := boundary.At(i), boundary.At((i+1)%boundary.Len())
		if h3.GreatCircleDistanceM(a, point) <= zone.RadiusM {
			return true
		}
		edge := newCellEdge(unit(a), unit(b))
		nearest := p.sub(edge.normal.scale(p.dot(edge.normal)))
		if location := nearest.latLng(); edge.contains(nearest) && zone.Contains(location.Lat.Deg(), location.Lng.Deg()) {
			return true
		}
	}
	return false
}

// Polygons dissolves adjacent cells into polygon rings, keeping unclaimed
// enclaves and other riders' cells as holes. Longitudes are unwrapped around
// the exterior ring so MapLibre can tile territories crossing the date line.
func Polygons(cells []Cell) ([][][][2]float64, error) {
	polygons, err := h3.CellsToMultiPolygon(cells)
	if err != nil {
		return nil, err
	}
	out := make([][][][2]float64, 0, len(polygons))
	for _, polygon := range polygons {
		outer := coordinates(polygon.GeoLoop, polygon.GeoLoop[0].Lng.Deg())
		rings := [][][2]float64{outer}
		for _, hole := range polygon.Holes {
			rings = append(rings, coordinates(hole, outer[0][0]))
		}
		out = append(out, rings)
	}
	return out, nil
}

func coordinates(vertices []h3.LatLng, origin float64) [][2]float64 {
	ring := make([][2]float64, 0, len(vertices)+1)
	for _, p := range vertices {
		origin += math.Remainder(p.Lng.Deg()-origin, 360)
		ring = append(ring, [2]float64{origin, p.Lat.Deg()})
	}
	return append(ring, ring[0])
}
