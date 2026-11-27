package territory

import (
	"math"
	"testing"
	"time"

	h3 "github.com/dimchansky/h3-go"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

func TestDistanceSplitsAtH3Boundary(t *testing.T) {
	cell := must(CellAt(51.2, 4.4))
	edges := must(cell.DirectedEdges())
	edge := edges[0]
	neighbor := must(edge.Destination())
	boundary := must(edge.Boundary())
	a, b := unit(boundary.At(0)), unit(boundary.At(1))
	mid := a.add(b).scale(0.5)
	n := a.cross(b)
	n = n.scale(1 / math.Sqrt(n.dot(n)))

	// Travel about 50 m on one side and 150 m on the other. Neither
	// sample is on the border, and the endpoints have unequal weights.
	from := mid.add(n.scale(0.000008)).latLng()
	to := mid.sub(n.scale(0.000024)).latLng()
	start := must(from.Cell(Resolution))
	end := must(to.Cell(Resolution))
	if start == end || (start != cell && end != cell) || (start != neighbor && end != neighbor) {
		t.Fatalf("test endpoints should straddle the edge: %s %s", start, end)
	}
	now := time.Now()
	samples := []domain.StreamSample{sampleAt(now, from, 1), sampleAt(now.Add(10*time.Second), to, 2)}
	meters := must(Distances(samples))
	atEdge := mid.latLng()
	if len(meters) != 2 ||
		math.Abs(float64(meters[start])/1000-h3.GreatCircleDistanceM(from, atEdge)) > .001 ||
		math.Abs(float64(meters[end])/1000-h3.GreatCircleDistanceM(atEdge, to)) > .001 {
		t.Fatalf("wrong distance split: %v", meters)
	}
}

func TestDistanceDiscardsGapsAndJumps(t *testing.T) {
	now := time.Now()
	points := []struct {
		second, index int
		lon           float64
	}{
		{0, 1, 4.0}, {1, 2, 4.0001}, // Valid (about 7 m).
		{1, 3, 4.0002},  // Duplicate timestamp.
		{60, 4, 4.001},  // Time gap.
		{61, 6, 4.0011}, // Recording-index gap.
		{62, 7, 4.02},   // Impossible speed.
		{63, 8, 4.0201}, // Valid again.
	}
	samples := make([]domain.StreamSample, 0, len(points))
	for _, point := range points {
		samples = append(samples, sampleAt(now.Add(time.Duration(point.second)*time.Second),
			h3.LatLngDegs(51, point.lon), point.index))
	}
	distances := must(Distances(samples))
	var total int64
	for _, mm := range distances {
		total += mm
	}
	want := 2 * h3.GreatCircleDistanceM(h3.LatLngDegs(51, 4), h3.LatLngDegs(51, 4.0001))
	if math.Abs(float64(total)/1000-want) > .002 {
		t.Fatalf("accumulated %d mm, want %.3f m", total, want)
	}
}

func TestDissolveKeepsHolesAndDropsInteriorBorders(t *testing.T) {
	center := must(CellAt(51.2, 4.4))
	ring := must(center.GridRing(1))
	polygons := must(Polygons(ring))
	if len(polygons) != 1 || len(polygons[0]) != 2 || len(polygons[0][0]) != 19 || len(polygons[0][1]) != 7 {
		t.Fatalf("expected a single 18-edge outline and one hexagonal hole: %v", polygons)
	}
	polygons = must(Polygons(append(ring, center)))
	if len(polygons) != 1 || len(polygons[0]) != 1 || len(polygons[0][0]) != 19 {
		t.Fatalf("adding the center must remove all interior edges: %v", polygons)
	}
	zone := domain.PrivacyZone{Lat: 51.2, Lng: 4.4, RadiusM: 10}
	if !must(Masked(center, []domain.PrivacyZone{zone})) {
		t.Fatal("a small zone inside the hexagon must hide that rider's contribution")
	}
	boundary := must(center.Boundary())
	onBoundary := unit(boundary.At(0)).add(unit(boundary.At(1))).latLng()
	if !must(Masked(center, []domain.PrivacyZone{
		{Lat: onBoundary.Lat.Deg(), Lng: onBoundary.Lng.Deg(), RadiusM: 10},
	})) {
		t.Fatal("a zone overlapping an edge between vertices must hide the hexagon")
	}
}

func TestTerritoryAtDateLine(t *testing.T) {
	a, b := h3.LatLngDegs(51, 179.998), h3.LatLngDegs(51, -179.998)
	now := time.Now()
	cells := must(Distances([]domain.StreamSample{sampleAt(now, a, 1), sampleAt(now.Add(12*time.Second), b, 2)}))
	var ids []Cell
	var total int64
	for cell, mm := range cells {
		ids = append(ids, cell)
		total += mm
	}
	if math.Abs(float64(total)/1000-h3.GreatCircleDistanceM(a, b)) > .002 {
		t.Fatalf("distance inflated across the date line: %v", cells)
	}
	polygons := must(Polygons(ids))
	for _, polygon := range polygons {
		for _, ring := range polygon {
			for i := 1; i < len(ring); i++ {
				if math.Abs(ring[i][0]-ring[i-1][0]) > .05 {
					t.Fatalf("polygon wraps around the world: %v", ring)
				}
			}
		}
	}
}

func sampleAt(ts time.Time, location h3.LatLng, index int) domain.StreamSample {
	return domain.StreamSample{Timestamp: ts, Latitude: new(location.Lat.Deg()),
		Longitude: new(location.Lng.Deg()), RecordingIndex: int32(index)}
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
