package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	cairnv1 "github.com/opencairn-org/cairn-core/gen/proto/cairn/v1"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/fileimport"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/protoconv"
)

const cacheLifetime = 7 * 24 * time.Hour

type catalog struct {
	RetrievedAt time.Time    `json:"retrieved_at"`
	Segments    []definition `json:"segments"`
}

type definition struct {
	ID                string            `json:"strava_id"`
	Name              string            `json:"name"`
	DistanceM         float64           `json:"strava_display_distance_m,omitempty"`
	ViewportDistanceM float64           `json:"viewport_distance_m,omitempty"`
	Geometry          []domain.GeoPoint `json:"geometry"`
	SourceURL         string            `json:"source_url"`
}

type effort struct {
	StartSample    int       `json:"start_sample"`
	EndSample      int       `json:"end_sample"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	ElapsedSeconds float64   `json:"elapsed_seconds"`
}

type candidate struct {
	ID                string   `json:"strava_id"`
	Name              string   `json:"name"`
	DistanceM         float64  `json:"strava_display_distance_m,omitempty"`
	ViewportDistanceM float64  `json:"viewport_distance_m,omitempty"`
	GeometryLengthM   float64  `json:"geometry_length_m"`
	GeometryPoints    int      `json:"geometry_points"`
	StravaURL         string   `json:"strava_url"`
	SourceURL         string   `json:"source_url"`
	Efforts           []effort `json:"efforts"`
}

func main() {
	activity := flag.String("activity", "", "archived FIT or GPX for a cycling activity")
	cachePath := flag.String("cache", "tmp/strava-segments/catalog.json", "local segment definition cache")
	refresh := flag.Bool("refresh", false, "retrieve the public regional catalog over HTTPS")
	viewport := flag.String("viewport", "", "local JSON response captured from a public Tailwind KOMpass map viewport")
	flag.Parse()
	var err error
	switch {
	case *viewport != "" && *refresh:
		err = errors.New("-viewport and -refresh select different sources")
	case *viewport != "":
		err = runViewport(*activity, *viewport, os.Stdout)
	default:
		err = run(*activity, *cachePath, *refresh, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(activity, cachePath string, refresh bool, output io.Writer) error {
	samples, err := readActivity(activity)
	if err != nil {
		return err
	}
	var data catalog
	if refresh {
		data, err = fetchCatalog()
		if err == nil {
			err = writeCache(cachePath, data)
		}
	} else {
		data, err = readCache(cachePath)
	}
	if err != nil {
		return fmt.Errorf("segment catalog: %w", err)
	}

	report := struct {
		Samples         int         `json:"activity_samples"`
		CacheOrigin     string      `json:"catalog_access"`
		RetrievedAt     time.Time   `json:"catalog_retrieved_at"`
		CatalogSegments int         `json:"catalog_segments"`
		Candidates      []candidate `json:"candidates"`
	}{
		Samples: len(samples), CacheOrigin: "offline-cache",
		RetrievedAt: data.RetrievedAt, CatalogSegments: len(data.Segments),
		Candidates: discover(samples, data.Segments),
	}
	if refresh {
		report.CacheOrigin = "live-refresh"
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func readActivity(filename string) ([]domain.StreamSample, error) {
	extension := strings.ToLower(filepath.Ext(filename))
	if extension != ".fit" && extension != ".gpx" {
		return nil, errors.New("provide a .fit or .gpx file with -activity")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, port.MaxUploadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > port.MaxUploadBytes {
		return nil, errors.New("activity exceeds Peloton's upload limit")
	}
	payload, stream, err := fileimport.Parse(filename, raw)
	if err != nil {
		return nil, err
	}
	if payload.GetType() != cairnv1.ActivityType_ACTIVITY_TYPE_RIDE &&
		!(extension == ".gpx" && payload.GetProviderNativeType() == "") {
		return nil, errors.New("activity is not a cycling ride")
	}
	if len(stream.GetTimeS()) != int(stream.GetSampleCount()) {
		return nil, errors.New("activity must contain original sample timestamps")
	}
	samples := protoconv.ActivityStreamSamplesFromProto(stream, payload.GetStartTime().AsTime())
	numGPS := 0
	for i, sample := range samples {
		if _, ok := gps(sample); ok {
			numGPS++
		} else {
			samples[i].Latitude, samples[i].Longitude = nil, nil
		}
		if i > 0 && sample.Timestamp.Before(samples[i-1].Timestamp) {
			return nil, errors.New("activity timestamps must be in recorded order")
		}
	}
	if numGPS < 2 {
		return nil, errors.New("activity needs at least two timestamped GPS points")
	}
	return samples, nil
}

func readCache(path string) (catalog, error) {
	var data catalog
	file, err := os.Open(path)
	if err != nil {
		return data, err
	}
	defer file.Close()
	if err := json.NewDecoder(file).Decode(&data); err != nil {
		return data, err
	}
	age := time.Since(data.RetrievedAt)
	if age < -time.Minute || age >= cacheLifetime || len(data.Segments) == 0 {
		return data, errors.New("cache is empty, expired, or has an invalid date. Run with -refresh")
	}
	for _, segment := range data.Segments {
		if err := segment.validate(); err != nil {
			return data, err
		}
	}
	return data, nil
}

func writeCache(path string, data catalog) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".segment-catalog-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	writeErr := json.NewEncoder(file).Encode(data)
	if closeErr := file.Close(); closeErr != nil || writeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	return os.Rename(file.Name(), path)
}
