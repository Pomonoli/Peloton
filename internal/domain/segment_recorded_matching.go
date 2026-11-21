package domain

import (
	"math"
	"time"
)

const (
	matchGapM        = 90
	matchGapTime     = 30 * time.Second
	resampleM        = 8
	hintCorridorM    = 50
	orderedCorridorM = 25
)

func recordedSamplePoint(sample StreamSample) (GeoPoint, bool) {
	p, ok := samplePoint(sample)
	return p, ok && math.Abs(p.Lat) <= 90 && math.Abs(p.Lon) <= 180
}

// MatchRecordedSegment verifies a directed traversal against the complete
// catalog geometry and times it using original stream samples.
func MatchRecordedSegment(samples []StreamSample, line []GeoPoint) []SegmentMatch {
	if len(line) < 2 {
		return nil
	}
	result := make([]SegmentMatch, 0)
	tolerances := DefaultMatchTolerances()
	// The Cairn walk can switch edges early around sparsely mapped corners.
	// Use it to find traversal windows and let the ordered check decide.
	tolerances.CorridorM = hintCorridorM
	var geometry []GeoPoint
	for _, found := range MatchSegment(samples, line, tolerances) {
		startIndex := closestSample(samples[:found.EndOffset], found.StartOffset, line[0], tolerances.StartToleranceM)
		endIndex := closestSample(samples, found.EndOffset, line[len(line)-1], tolerances.EndToleranceM)
		start, end := samples[startIndex].Timestamp, samples[endIndex].Timestamp
		if endIndex <= startIndex || !end.After(start) {
			continue
		}
		track := continuousTrack(samples[startIndex : endIndex+1])
		if len(track) < 2 {
			continue
		}
		if geometry == nil {
			geometry = densify(line)
		}
		if !orderedCorridor(densify(track), geometry, orderedCorridorM) {
			continue
		}
		result = append(result, SegmentMatch{
			StartOffset: startIndex, EndOffset: endIndex, ElapsedSeconds: end.Sub(start).Seconds(),
		})
	}
	return result
}

// Choose the closest original GPS sample within this encounter with the endpoint.
func closestSample(samples []StreamSample, first int, endpoint GeoPoint, radiusM float64) int {
	closest, shortest := first, math.Inf(1)
	for i := first; i < len(samples); i++ {
		point, ok := recordedSamplePoint(samples[i])
		if !ok {
			break
		}
		distance := HaversineMeters(point, endpoint)
		if distance > radiusM {
			break
		}
		if distance < shortest {
			closest, shortest = i, distance
		}
	}
	return closest
}

func continuousTrack(samples []StreamSample) []GeoPoint {
	track := make([]GeoPoint, 0, len(samples))
	var lastTime time.Time
	for _, sample := range samples {
		point, ok := recordedSamplePoint(sample)
		if !ok {
			continue
		}
		if len(track) > 0 && ((!sample.Timestamp.After(lastTime) || sample.Timestamp.Sub(lastTime) > matchGapTime) ||
			HaversineMeters(track[len(track)-1], point) > matchGapM) {
			return nil
		}
		track = append(track, point)
		lastTime = sample.Timestamp
	}
	return track
}

func densify(line []GeoPoint) []GeoPoint {
	points := []GeoPoint{line[0]}
	for i, b := range line[1:] {
		a := line[i]
		steps := int(math.Ceil(HaversineMeters(a, b) / resampleM))
		for k := 1; k < steps; k++ {
			fraction := float64(k) / float64(steps)
			points = append(points, GeoPoint{
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
func orderedCorridor(track, segment []GeoPoint, toleranceM float64) bool {
	reachable := make([]bool, len(segment))
	for i, a := range track {
		var left, diagonal bool
		any := false
		for j, b := range segment {
			up := reachable[j]
			reachable[j] = (up || left || diagonal || (i == 0 && j == 0)) &&
				HaversineMeters(a, b) <= toleranceM
			left, diagonal = reachable[j], up
			any = any || left
		}
		if !any {
			return false
		}
	}
	return reachable[len(segment)-1]
}
