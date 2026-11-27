package domain

import (
	"slices"
	"testing"
)

func TestRecordedRouteMapCountsUniqueRoutes(t *testing.T) {
	// A and B share a road in opposite directions, offset by about 3 m.
	// A traverses it twice. C is a parallel road about 11 m away and D
	// intersects at right angles.
	a := RecordedTrack{{
		{Lat: 51, Lon: 4}, {Lat: 51.000008, Lon: 4.001}, {Lat: 51, Lon: 4.004}, {Lat: 51, Lon: 4},
	}}
	b := RecordedTrack{{{Lat: 51.000026, Lon: 4.003}, {Lat: 51.000026, Lon: 4.001}}}
	c := RecordedTrack{{{Lat: 51.0001, Lon: 4}, {Lat: 51.0001, Lon: 4.004}}}
	d := RecordedTrack{{{Lat: 50.999, Lon: 4.002}, {Lat: 51.001, Lon: 4.002}}}

	tracks := []RecordedTrack{a.MapGeometry(), b, c, d}
	foundOverlap, foundUnshared := false, false
	mapped, err := RecordedRouteMap(t.Context(), tracks)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range mapped {
		switch {
		case slices.Equal(line.Routes, []int{0, 1}):
			foundOverlap = true
		case slices.Equal(line.Routes, []int{0}):
			foundUnshared = true
		case slices.Equal(line.Routes, []int{1}), slices.Equal(line.Routes, []int{2}), slices.Equal(line.Routes, []int{3}):
		default:
			t.Fatalf("unexpected matches %v", line.Routes)
		}
		if len(line.Points) < 2 {
			t.Fatalf("invalid line %v", line.Points)
		}
	}
	if !foundOverlap || !foundUnshared {
		t.Fatalf("shared and unshared sections must both be present: %t, %t", foundOverlap, foundUnshared)
	}
	filtered, err := RecordedRouteMap(t.Context(), []RecordedTrack{a.MapGeometry(), c, d})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range filtered {
		if len(line.Routes) != 1 {
			t.Fatalf("filtering B must remove the overlap: %v", line.Routes)
		}
	}
}

func TestRecordedRouteMapKeepsGapsAndWrapsDateline(t *testing.T) {
	a := RecordedTrack{
		{{Lat: 50, Lon: 179.999}, {Lat: 50, Lon: -179.999}},
		{{Lat: 50, Lon: -179.994}, {Lat: 50, Lon: -179.993}},
	}
	b := RecordedTrack{{{Lat: 50.000015, Lon: -179.999}, {Lat: 50.000015, Lon: 179.999}}}
	out, err := RecordedRouteMap(t.Context(), []RecordedTrack{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || !slices.Equal(out[0].Routes, []int{0, 1}) ||
		!slices.Equal(out[1].Routes, []int{0}) || !slices.Equal(out[2].Routes, []int{0, 1}) {
		t.Fatalf("incorrect matching across the dateline or gap: %+v", out)
	}
	if len(out[0].Points) != 2 || len(out[1].Points) != 2 {
		t.Fatalf("internal matching vertices leaked into output: %+v", out)
	}
}

func BenchmarkRecordedRouteMap(b *testing.B) {
	// A 2,500 km catalog with an overlapping corridor shared by 250 routes.
	tracks := make([]RecordedTrack, 250)
	for i := range tracks {
		lat := 51 + float64(i%5)*.000005
		tracks[i] = RecordedTrack{{
			{Lat: lat, Lon: 4}, {Lat: lat, Lon: 4.015},
			{Lat: lat + .005 + float64(i/5)*.0003, Lon: 4.03},
			{Lat: lat + .006 + float64(i/5)*.0004, Lon: 4.1},
		}}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := RecordedRouteMap(b.Context(), tracks); err != nil {
			b.Fatal(err)
		}
	}
}
