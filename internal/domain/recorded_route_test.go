package domain

import (
	"math"
	"testing"
)

func TestRecordedRouteCoverage(t *testing.T) {
	straight := func(from, to, north float64) RecordedTrack {
		degrees := 180 / (math.Pi * EarthRadiusMeters)
		var line []RecordedRoutePoint
		for meters := from; meters <= to; meters += 10 {
			line = append(line, RecordedRoutePoint{Lat: 50 + north*degrees, Lon: 4 + meters*degrees/math.Cos(50*math.Pi/180)})
		}
		return RecordedTrack{line}
	}
	a := straight(0, 4000, 0)
	b := straight(400, 4400, 0)
	c := straight(800, 4800, 0)
	if a.Coverage().RecordedOverlap(b.Coverage()) < MinRecordedRouteOverlap ||
		b.Coverage().RecordedOverlap(c.Coverage()) < MinRecordedRouteOverlap ||
		a.Coverage().RecordedOverlap(c.Coverage()) >= MinRecordedRouteOverlap {
		t.Fatal("the overlapping chain must not pass a direct comparison to its initial route")
	}
	if score := a.Coverage().RecordedOverlap(straight(0, 4000, 18).Coverage()); score < .98 {
		t.Fatalf("minor GPS offset should match, overlap = %v", score)
	}
	if score := a.Coverage().RecordedOverlap(straight(0, 4000, 72).Coverage()); score != 0 {
		t.Fatalf("nearby road should not match, overlap = %v", score)
	}

	reversed := append([]RecordedRoutePoint(nil), a[0]...)
	for i := range len(reversed) / 2 {
		reversed[i], reversed[len(reversed)-1-i] = reversed[len(reversed)-1-i], reversed[i]
	}
	other := RecordedTrack{reversed}.Coverage()
	if score := a.Coverage().RecordedOverlap(other); score < .99 {
		t.Fatalf("reverse directions should match, overlap = %v", score)
	}
	if a.Coverage().Overlap(other) >= MinRouteOverlap {
		t.Fatal("activity Similar Routes must remain directional")
	}
	outAndBack := RecordedTrack{append(a[0], reversed[1:]...)}
	if outAndBack.Kind(8000, outAndBack.Coverage()) != RecordedOutAndBack {
		t.Fatal("repeated return corridor should be classified as out and back")
	}
}

func TestRecordedRouteMaskSplitsCrossing(t *testing.T) {
	track := RecordedTrack{{
		{Lat: 51, Lon: 4}, {Lat: 51, Lon: 4.001},
		{Lat: 51, Lon: 4.004}, {Lat: 51, Lon: 4.005},
	}}
	masked := track.Mask([]PrivacyZone{{Lat: 51, Lng: 4.0025, RadiusM: 40}})
	if len(masked) != 2 || len(masked[0]) != 2 || len(masked[1]) != 2 {
		t.Fatalf("segment across a private area must be broken: %+v", masked)
	}
}
