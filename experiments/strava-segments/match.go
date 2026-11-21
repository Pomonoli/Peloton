package main

import (
	"math"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

const (
	bboxBufferM = 250
)

func discover(samples []domain.StreamSample, segments []definition) []candidate {
	result := make([]candidate, 0)
	minLat, maxLat, minLon, maxLon, ok := domain.SampleBoundingBox(samples)
	if !ok {
		return result
	}
	minLat, maxLat, minLon, maxLon = domain.ExpandBoundingBox(minLat, maxLat, minLon, maxLon, bboxBufferM)
	for _, segment := range segments {
		south, north, west, east := bounds(segment.Geometry)
		if south > maxLat || north < minLat || west > maxLon || east < minLon {
			continue
		}
		result = append(result, candidate{
			ID: segment.ID, Name: segment.Name, DistanceM: segment.DistanceM,
			ViewportDistanceM: segment.ViewportDistanceM,
			GeometryLengthM:   math.Round(lineLength(segment.Geometry)),
			GeometryPoints:    len(segment.Geometry), StravaURL: stravaURL + segment.ID,
			SourceURL: segment.SourceURL, Efforts: match(samples, segment.Geometry),
		})
	}
	return result
}

func lineLength(line []domain.GeoPoint) float64 {
	var meters float64
	for i, point := range line[1:] {
		meters += domain.HaversineMeters(line[i], point)
	}
	return meters
}

func bounds(line []domain.GeoPoint) (south, north, west, east float64) {
	south, north = line[0].Lat, line[0].Lat
	west, east = line[0].Lon, line[0].Lon
	for _, point := range line[1:] {
		south, north = min(south, point.Lat), max(north, point.Lat)
		west, east = min(west, point.Lon), max(east, point.Lon)
	}
	return south, north, west, east
}

func gps(sample domain.StreamSample) (domain.GeoPoint, bool) {
	if sample.Latitude == nil || sample.Longitude == nil {
		return domain.GeoPoint{}, false
	}
	point := domain.GeoPoint{Lat: *sample.Latitude, Lon: *sample.Longitude}
	return point, validPoint(point)
}

func validPoint(point domain.GeoPoint) bool {
	return math.Abs(point.Lat) <= 90 && math.Abs(point.Lon) <= 180
}

func match(samples []domain.StreamSample, line []domain.GeoPoint) []effort {
	result := make([]effort, 0)
	for _, m := range domain.MatchRecordedSegment(samples, line) {
		result = append(result, effort{
			StartSample: m.StartOffset, EndSample: m.EndOffset,
			StartTime: samples[m.StartOffset].Timestamp, EndTime: samples[m.EndOffset].Timestamp,
			ElapsedSeconds: m.ElapsedSeconds,
		})
	}
	return result
}
