package main

import (
	"math"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

const (
	bboxBufferM      = 250
	matchGapM        = 90
	matchGapTime     = 30 * time.Second
	resampleM        = 8
	hintCorridorM    = 50
	orderedCorridorM = 25
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
	tolerances := domain.DefaultMatchTolerances()
	// The Cairn walk can switch edges early around sparsely mapped corners.
	// Use it to find traversal windows and let the ordered check decide.
	tolerances.CorridorM = hintCorridorM
	geometry := densify(line)
	for _, found := range domain.MatchSegment(samples, line, tolerances) {
		startIndex := closestSample(samples[:found.EndOffset], found.StartOffset, line[0], tolerances.StartToleranceM)
		endIndex := closestSample(samples, found.EndOffset, line[len(line)-1], tolerances.EndToleranceM)
		start, end := samples[startIndex].Timestamp, samples[endIndex].Timestamp
		if endIndex <= startIndex || !end.After(start) {
			continue
		}
		track := continuousTrack(samples[startIndex : endIndex+1])
		if len(track) < 2 || !orderedCorridor(densify(track), geometry, orderedCorridorM) {
			continue
		}
		result = append(result, effort{
			StartSample: startIndex, EndSample: endIndex,
			StartTime: start, EndTime: end, ElapsedSeconds: end.Sub(start).Seconds(),
		})
	}
	return result
}

// Choose the closest original GPS sample within this encounter with the endpoint.
func closestSample(samples []domain.StreamSample, first int, endpoint domain.GeoPoint, radiusM float64) int {
	closest, shortest := first, math.Inf(1)
	for i := first; i < len(samples); i++ {
		point, ok := gps(samples[i])
		if !ok {
			break
		}
		distance := domain.HaversineMeters(point, endpoint)
		if distance > radiusM {
			break
		}
		if distance < shortest {
			closest, shortest = i, distance
		}
	}
	return closest
}

func continuousTrack(samples []domain.StreamSample) []domain.GeoPoint {
	track := make([]domain.GeoPoint, 0, len(samples))
	var lastTime time.Time
	for _, sample := range samples {
		point, ok := gps(sample)
		if !ok {
			continue
		}
		if len(track) > 0 && (sample.Timestamp.Sub(lastTime) > matchGapTime ||
			domain.HaversineMeters(track[len(track)-1], point) > matchGapM) {
			return nil
		}
		track = append(track, point)
		lastTime = sample.Timestamp
	}
	return track
}

func densify(line []domain.GeoPoint) []domain.GeoPoint {
	points := []domain.GeoPoint{line[0]}
	for i, b := range line[1:] {
		a := line[i]
		steps := int(math.Ceil(domain.HaversineMeters(a, b) / resampleM))
		for k := 1; k < steps; k++ {
			fraction := float64(k) / float64(steps)
			points = append(points, domain.GeoPoint{
				Lat: a.Lat + (b.Lat-a.Lat)*fraction,
				Lon: a.Lon + (b.Lon-a.Lon)*fraction,
			})
		}
		points = append(points, b)
	}
	return points
}

// The discrete Fréchet decision check pairs both complete polylines in order.
// Either side may advance by one point at a time. Every point on both sides
// must be covered, so a nearby start and finish cannot hide a shortcut.
func orderedCorridor(track, segment []domain.GeoPoint, toleranceM float64) bool {
	reachable := make([]bool, len(segment))
	for i, a := range track {
		var left, diagonal bool
		any := false
		for j, b := range segment {
			up := reachable[j]
			reachable[j] = (up || left || diagonal || (i == 0 && j == 0)) &&
				domain.HaversineMeters(a, b) <= toleranceM
			left, diagonal = reachable[j], up
			any = any || left
		}
		if !any {
			return false
		}
	}
	return reachable[len(segment)-1]
}
