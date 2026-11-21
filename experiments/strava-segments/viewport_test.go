package main

import (
	"encoding/json"
	"testing"

	"github.com/twpayne/go-polyline"
)

func TestParseViewportKeepsVerifiedGeometryOnce(t *testing.T) {
	makeRow := func(id json.Number, endLat float64) viewportRow {
		startLat, startLon, endLon := 51.16, 4.14, 4.14
		return viewportRow{
			ID: id, Name: "Fixture segment", Distance: 220,
			Polyline: string(polyline.EncodeCoords([][]float64{{startLat, startLon}, {51.162, endLon}})),
			StartLat: &startLat, StartLon: &startLon, EndLat: &endLat, EndLon: &endLon,
		}
	}
	data, err := json.Marshal([]viewportRow{
		makeRow("42", 51.176), // An endpoint mismatch must not hide the next valid copy.
		makeRow("42", 51.162),
		makeRow("42", 51.162),
		makeRow("43", 51.176),
		{ID: "44", Name: "Missing geometry", Distance: 200},
	})
	if err != nil {
		t.Fatal(err)
	}
	segments, rows, err := parseViewport(data)
	if err != nil || rows != 5 || len(segments) != 1 {
		t.Fatalf("parseViewport got %d rows and %+v segments (error %v)", rows, segments, err)
	}
	segment := segments[0]
	if segment.ID != "42" || segment.ViewportDistanceM != 220 || segment.SourceURL != viewportURL ||
		len(segment.Geometry) != 2 || segment.Geometry[1].Lat != 51.162 {
		t.Fatalf("unexpected viewport segment %+v", segment)
	}
}

func TestParseViewportDistinguishesEmptyFromSchemaFailure(t *testing.T) {
	for _, test := range []struct {
		data string
		fail bool
	}{
		{data: `[]`},
		{data: `null`, fail: true},
		{data: `{"error":"Unavailable"}`, fail: true},
		{data: `[{"id":43,"name":"Incomplete geometry","distance":220}]`, fail: true},
	} {
		segments, _, err := parseViewport([]byte(test.data))
		if len(segments) != 0 || (err != nil) != test.fail {
			t.Fatalf("parseViewport(%s) got %+v, %v", test.data, segments, err)
		}
	}
}
