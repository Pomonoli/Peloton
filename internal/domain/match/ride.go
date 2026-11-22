package match

import (
	"math"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// RideRoute compares progress through rides independently of GPS sampling frequency.
type RideRoute []domain.GeoPoint

// RideRouteFromStream samples GPS at evenly spaced points in the recording.
func RideRouteFromStream(samples []domain.StreamSample) RideRoute {
	gps := make([]domain.StreamSample, 0, len(samples))
	for _, s := range samples {
		if s.Latitude != nil && s.Longitude != nil {
			gps = append(gps, s)
		}
	}
	if len(gps) < 10 {
		return nil
	}
	const checkpoints = 10
	start := gps[0].Timestamp
	length := gps[len(gps)-1].Timestamp.Sub(start)
	route := make(RideRoute, 0, checkpoints-1)
	j := 1
	for i := 1; i < checkpoints; i++ {
		at := start.Add(length * time.Duration(i) / checkpoints)
		for j < len(gps)-1 && gps[j].Timestamp.Before(at) {
			j++
		}
		before, after := gps[j-1], gps[j]
		interval := after.Timestamp.Sub(before.Timestamp)
		if interval <= 0 || interval > 90*time.Second {
			return nil // too much of the track is missing to compare progress
		}
		f := float64(at.Sub(before.Timestamp)) / float64(interval)
		route = append(route, domain.GeoPoint{
			Lat: *before.Latitude + f*(*after.Latitude-*before.Latitude),
			Lon: *before.Longitude + f*(*after.Longitude-*before.Longitude),
		})
	}
	return route
}

// RideImportCandidate uses hard temporal and scalar bounds before reading GPS.
func RideImportCandidate(a, b Record) bool {
	ga, _ := sportGroup(a.SportClass)
	gb, _ := sportGroup(b.SportClass)
	delta := a.StartUTC.Sub(b.StartUTC).Abs()
	if ga != "ride" || gb != "ride" || delta > 3*time.Minute {
		return false
	}
	if a.ElapsedS <= 0 || b.ElapsedS <= 0 {
		return false
	}
	shortest, longest := min(a.ElapsedS, b.ElapsedS), max(a.ElapsedS, b.ElapsedS)
	if float64(delta)/float64(time.Second) > float64(shortest)*0.25 || float64(shortest) < float64(longest)*0.75 {
		return false
	}
	return a.DistanceM <= 0 || b.DistanceM <= 0 ||
		math.Abs(a.DistanceM-b.DistanceM) <= max(150, 0.15*max(a.DistanceM, b.DistanceM))
}

// RideImportPair identifies cycling recordings subject to cross-import checks.
func RideImportPair(a, b Record) bool {
	ga, _ := sportGroup(a.SportClass)
	gb, _ := sportGroup(b.SportClass)
	crossFileImport := (a.Provider == "garmin" && b.Provider == "upload") ||
		(a.Provider == "upload" && b.Provider == "garmin")
	return (ga == "ride" || gb == "ride") &&
		(crossFileImport || domain.ProviderDataRestricted(a.Provider) || domain.ProviderDataRestricted(b.Provider))
}

func rideImportScore(a, b Record, w Weights) (float64, bool) {
	if !RideImportCandidate(a, b) {
		return 0, true
	}
	if len(a.Route) > 0 && len(a.Route) == len(b.Route) {
		matches := 0
		for i, p := range a.Route {
			q := b.Route[i]
			if haversineMeters(p.Lat, p.Lon, q.Lat, q.Lon) <= 450 {
				matches++
			}
		}
		if matches < 6 {
			return 0, true
		}
		// Route progression can recover recordings whose starts were trimmed.
		return 0.85 + 0.15*float64(matches)/float64(len(a.Route)), false
	}
	// Without comparable tracks, require near-identical times and duration.
	// This still covers indoor recordings and summaries with missing GPS.
	if a.StartUTC.Sub(b.StartUTC).Abs() > 20*time.Second ||
		float64(min(a.ElapsedS, b.ElapsedS)) < 0.9*float64(max(a.ElapsedS, b.ElapsedS)) {
		return 0, true
	}
	score, gated := Score(a.SourceFeatures, b.SourceFeatures, w)
	return score, gated || score < 0.82
}
