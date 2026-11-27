// Package territory measures validated cycling tracks in the H3 grid.
package territory

import (
	"math"
	"slices"

	h3 "github.com/dimchansky/h3-go"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

const Resolution = 8

type Cell = h3.Cell

func CellAt(lat, lon float64) (Cell, error) {
	return h3.LatLngToCell(h3.LatLngDegs(lat, lon), Resolution)
}

// Distances measures the GPS path in millimeters per cell. Device odometers
// help validate legs but cannot contribute distance that has no GPS location.
func Distances(samples []domain.StreamSample) (map[Cell]int64, error) {
	if len(samples) < 2 {
		return nil, nil
	}
	totals := make(map[Cell]float64)
	grid := make(map[Cell]cellGeometry)
	stream := domain.ValidateCyclingStream(samples)
	for i, sample := range samples[1:] {
		leg := stream.Legs[i+1]
		if !leg.Valid || !leg.HasGPS || leg.DistanceM <= 0 {
			continue
		}
		a := h3.LatLngDegs(*samples[i].Latitude, *samples[i].Longitude)
		b := h3.LatLngDegs(*sample.Latitude, *sample.Longitude)
		if err := splitLeg(grid, a, b, totals); err != nil {
			return nil, err
		}
	}

	distances := make(map[Cell]int64, len(totals))
	for cell, meters := range totals {
		if mm := int64(math.Round(meters * 1000)); mm > 0 {
			distances[cell] = mm
		}
	}
	return distances, nil
}

// A normalized chord follows the same great circle as the GPS leg. Cell
// edges are also great-circle arcs, including the extra vertices at H3 face
// boundaries. Find all edge intersections, then label the resulting intervals
// by their midpoint so a vertex touch or shared edge never double-counts.
func splitLeg(grid map[Cell]cellGeometry, a, b h3.LatLng, totals map[Cell]float64) error {
	start, err := a.Cell(Resolution)
	if err != nil {
		return err
	}
	end, err := b.Cell(Resolution)
	if err != nil {
		return err
	}
	from, to := unit(a), unit(b)
	delta := to.sub(from)
	if delta.dot(delta) == 0 {
		return nil
	}
	at := func(t float64) vector { return from.add(delta.scale(t)) }

	cuts, err := legCuts(grid, from, to, start, end)
	if err != nil {
		return err
	}

	if len(cuts) == 2 && start == end {
		totals[start] += h3.GreatCircleDistanceM(a, b)
		return nil
	}
	slices.Sort(cuts)
	for i := 1; i < len(cuts); i++ {
		lo, hi := cuts[i-1], cuts[i]
		if hi-lo <= 1e-9 {
			continue
		}
		cell, err := at((lo + hi) / 2).latLng().Cell(Resolution)
		if err != nil {
			return err
		}
		totals[cell] += h3.GreatCircleDistanceM(at(lo).latLng(), at(hi).latLng())
	}
	return nil
}

func legCuts(grid map[Cell]cellGeometry, from, to vector, start, end Cell) ([]float64, error) {
	queue := []Cell{start}
	if end != start {
		queue = append(queue, end)
	}
	cuts := []float64{0, 1}
	for i := 0; i < len(queue); i++ {
		cell := queue[i]
		geometry, err := cachedGeometry(grid, cell)
		if err != nil {
			return nil, err
		}
		previous := len(cuts)
		cuts = geometry.appendIntersections(cuts, from, to)
		if len(cuts) == previous {
			continue
		}
		for _, adjacent := range geometry.neighbors {
			if !slices.Contains(queue, adjacent) {
				queue = append(queue, adjacent)
			}
		}
	}
	return cuts, nil
}

func cachedGeometry(grid map[Cell]cellGeometry, cell Cell) (cellGeometry, error) {
	if geometry, ok := grid[cell]; ok {
		return geometry, nil
	}
	geometry, err := geometryFor(cell)
	if err == nil {
		grid[cell] = geometry
	}
	return geometry, err
}

type vector struct{ x, y, z float64 }

func unit(p h3.LatLng) vector {
	sinLat, cosLat := math.Sincos(p.Lat.Rad())
	sinLon, cosLon := math.Sincos(p.Lng.Rad())
	return vector{cosLat * cosLon, cosLat * sinLon, sinLat}
}

func (v vector) latLng() h3.LatLng {
	return h3.NewLatLng(h3.Rad(math.Atan2(v.z, math.Hypot(v.x, v.y))), h3.Rad(math.Atan2(v.y, v.x)))
}

func (v vector) add(w vector) vector    { return vector{v.x + w.x, v.y + w.y, v.z + w.z} }
func (v vector) sub(w vector) vector    { return vector{v.x - w.x, v.y - w.y, v.z - w.z} }
func (v vector) scale(k float64) vector { return vector{k * v.x, k * v.y, k * v.z} }
func (v vector) dot(w vector) float64   { return v.x*w.x + v.y*w.y + v.z*w.z }
func (v vector) cross(w vector) vector {
	return vector{v.y*w.z - v.z*w.y, v.z*w.x - v.x*w.z, v.x*w.y - v.y*w.x}
}

type cellEdge struct {
	start, end, normal vector
}

func newCellEdge(start, end vector) cellEdge {
	normal := start.cross(end)
	return cellEdge{start, end, normal.scale(1 / math.Sqrt(normal.dot(normal)))}
}

func (e cellEdge) contains(p vector) bool {
	const tolerance = 1e-12
	return e.start.cross(p).dot(e.normal) >= -tolerance && p.cross(e.end).dot(e.normal) >= -tolerance
}

type cellGeometry struct {
	edges     []cellEdge
	neighbors []Cell
}

func (g cellGeometry) appendIntersections(cuts []float64, from, to vector) []float64 {
	for _, edge := range g.edges {
		d0, d1 := edge.normal.dot(from), edge.normal.dot(to)
		if (d0 > 0 && d1 > 0) || (d0 < 0 && d1 < 0) || d0 == d1 {
			continue
		}
		t := d0 / (d0 - d1)
		if edge.contains(from.add(to.sub(from).scale(t))) {
			cuts = append(cuts, t)
		}
	}
	return cuts
}

func geometryFor(cell Cell) (cellGeometry, error) {
	boundary, err := cell.Boundary()
	if err != nil {
		return cellGeometry{}, err
	}
	neighbors, err := cell.GridDisk(1)
	if err != nil {
		return cellGeometry{}, err
	}
	geometry := cellGeometry{neighbors: neighbors, edges: make([]cellEdge, boundary.Len())}
	for i := range geometry.edges {
		start := unit(boundary.At(i))
		end := unit(boundary.At((i + 1) % boundary.Len()))
		geometry.edges[i] = newCellEdge(start, end)
	}
	return geometry, nil
}
