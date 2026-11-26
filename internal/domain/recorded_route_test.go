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
	b := straight(670, 4670, 0)
	c := straight(1340, 5340, 0)
	for _, pair := range [][2]RecordedTrack{{a, b}, {b, c}, {b, a}} {
		if score := pair[0].Coverage().RecordedOverlap(pair[1].Coverage()); score < .80 || score >= .85 {
			t.Fatalf("expected a match in the new 80–85%% range, got %v", score)
		}
	}
	if score := a.Coverage().RecordedOverlap(c.Coverage()); score >= MinRecordedRouteOverlap {
		t.Fatalf("distant endpoints of the overlapping chain matched, overlap = %v", score)
	}
	if score := a.Coverage().RecordedOverlap(straight(0, 3240, 0).Coverage()); score < .80 || score >= .85 {
		t.Fatalf("81%% length variation should match, overlap = %v", score)
	}
	if score := a.Coverage().RecordedOverlap(straight(0, 3190, 0).Coverage()); score != 0 {
		t.Fatalf("length ratio should reject a short track even within GPS tolerance, overlap = %v", score)
	}
	detour := append(straight(0, 1700, 0)[0], straight(1700, 2300, 100)[0]...)
	detour = append(detour, straight(2300, 4000, 0)[0]...)
	if score := a.Coverage().RecordedOverlap(RecordedTrack{detour}.Coverage()); score < .80 || score >= .85 {
		t.Fatalf("short diversion should match, overlap = %v", score)
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
