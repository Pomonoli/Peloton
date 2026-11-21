package main

import (
	"math"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

func TestParsePublicSegment(t *testing.T) {
	page := []byte(`
<div class="field--name-field-strava-segment-link field"><a href="https://www.strava.com/segments/42">View on Strava</a></div>
<section id="test-field-full-polyline-map"></section>
<script type="application/json" data-drupal-selector="drupal-settings-json">
{"leaflet":{"test-field-full-polyline-map":{"features":[
{"type":"point","lat":50.80,"lon":3.57},
{"type":"linestring","points":[{"lat":50.80,"lon":3.57},{"lat":50.81,"lon":3.58}]}
]}}}
</script>`)
	source := flandersURL + "/fixture"
	segment, err := parseDefinition(page, source)
	if err != nil {
		t.Fatal(err)
	}
	stravaPage := []byte(`<h1><a target="_parent" href="/segments/42?utm_content=42">Test &amp; cycling segment</a></h1>
<ul class='list-stats'><li><div class='stat'><div class='span stat-subtext'>Distance</div>
<b class='stat-text'>0.5 km</b></div></li></ul>`)
	if err := applyStravaEmbed(stravaPage, &segment); err != nil {
		t.Fatal(err)
	}
	if err := segment.validate(); err != nil {
		t.Fatal(err)
	}
	if segment.ID != "42" || segment.DistanceM != 500 || segment.Geometry[1].Lon != 3.58 ||
		segment.Name != "Test & cycling segment" || segment.SourceURL != source {
		t.Fatalf("unexpected definition: %+v", segment)
	}
	if err := applyStravaEmbed(stravaPage, &definition{ID: "43"}); err == nil {
		t.Fatal("accepted the wrong public Strava ID")
	}

	path := filepath.Join(t.TempDir(), "cache.json")
	now := time.Now().UTC()
	if err := writeCache(path, catalog{RetrievedAt: now, Segments: []definition{segment}}); err != nil {
		t.Fatal(err)
	}
	data, err := readCache(path)
	if err != nil || len(data.Segments) != 1 || !slices.Equal(data.Segments[0].Geometry, segment.Geometry) {
		t.Fatalf("cache did not preserve geometry: %+v, %v", data, err)
	}
	if err := writeCache(path, catalog{RetrievedAt: now.Add(-cacheLifetime), Segments: []definition{segment}}); err != nil {
		t.Fatal(err)
	}
	if _, err := readCache(path); err == nil {
		t.Fatal("accepted expired definitions")
	}
}

func TestDiscoverMatchesGeometryAndOriginalTime(t *testing.T) {
	line := []domain.GeoPoint{{Lat: 51, Lon: 4}, {Lat: 51.0015, Lon: 4}, {Lat: 51.0015, Lon: 4.0015}}
	segment := definition{ID: "42", Name: "Test corner", Geometry: line}
	distant := definition{ID: "43", Geometry: []domain.GeoPoint{{Lat: 52, Lon: 4}, {Lat: 52.001, Lon: 4}}}
	forward := densify(line)
	reverse := slices.Clone(forward)
	slices.Reverse(reverse)
	shortcut := densify([]domain.GeoPoint{line[0], line[len(line)-1]})
	forwardSamples := testSamples(forward)
	for _, tt := range []struct {
		name    string
		samples []domain.StreamSample
		n       int
	}{
		{name: "forward", samples: forwardSamples, n: 1},
		{name: "reverse", samples: testSamples(reverse), n: 0},
		{name: "shortcut with the same endpoints", samples: testSamples(shortcut), n: 0},
		{name: "GPS dropout across the segment", samples: []domain.StreamSample{forwardSamples[0], forwardSamples[len(forwardSamples)-1]}, n: 0},
		{name: "missing finish", samples: forwardSamples[:len(forwardSamples)*3/4], n: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := discover(tt.samples, []definition{segment, distant})
			if len(got) != 1 || got[0].ID != "42" || len(got[0].Efforts) != tt.n {
				t.Fatalf("expected one candidate and %d efforts, got %+v", tt.n, got)
			}
			if tt.n == 1 {
				match := got[0].Efforts[0]
				first, last := tt.samples[0].Timestamp, tt.samples[len(tt.samples)-1].Timestamp
				if !match.StartTime.Equal(first) || !match.EndTime.Equal(last) ||
					math.Abs(match.ElapsedSeconds-last.Sub(first).Seconds()) > 1e-9 {
					t.Fatalf("did not use original endpoint samples: %+v", match)
				}
			}
		})
	}
}

func testSamples(line []domain.GeoPoint) []domain.StreamSample {
	start := time.Date(2025, time.December, 1, 13, 0, 0, 0, time.UTC)
	samples := make([]domain.StreamSample, 0, len(line))
	for i, point := range line {
		offset := time.Duration(i)*1500*time.Millisecond + time.Duration(i%3)*100*time.Millisecond
		samples = append(samples, domain.StreamSample{
			Latitude: &point.Lat, Longitude: &point.Lon, Timestamp: start.Add(offset),
		})
	}
	return samples
}
