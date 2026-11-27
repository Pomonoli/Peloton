package domain

import (
	"math"
	"slices"
	"testing"
	"time"
)

var proximityTestStart = time.Date(2022, 6, 1, 9, 0, 0, 0, time.UTC)

func proximityTrack(from, to, step int, position func(float64) (north, east float64)) []StreamSample {
	var samples []StreamSample
	const degrees = 180 / (math.Pi * EarthRadiusMeters)
	for second := from; second <= to; second += step {
		north, east := position(float64(second))
		samples = append(samples, StreamSample{
			Timestamp: proximityTestStart.Add(time.Duration(second) * time.Second),
			Latitude:  new(51 + north*degrees), Longitude: new(4 + east*degrees/math.Cos(51*math.Pi/180)),
		})
	}
	return samples
}

func TestCompareRideTracksPartialOverlapAndRejoin(t *testing.T) {
	a := proximityTrack(0, 1000, 1, func(t float64) (float64, float64) { return 8 * t, 0 })
	b := proximityTrack(101, 901, 4, func(t float64) (float64, float64) { return 8 * t, 14 })
	// The missing section is a recording gap. A second section can still
	// contribute to the ride, but the time and distance across the gap cannot.
	b = slices.DeleteFunc(b, func(s StreamSample) bool {
		offset := s.Timestamp.Sub(proximityTestStart).Seconds()
		return offset > 441 && offset < 541
	})
	got := CompareRideTracks(a, b, nil, nil)
	if got.Shared == nil || math.Abs(got.Shared.DistanceM-5600) > 2 || got.Shared.Seconds != 700 || len(got.Encounters) != 0 {
		t.Fatalf("incorrect partial shared ride: %+v, %+v", got.Shared, got.Encounters)
	}
	reverse := CompareRideTracks(b, a, nil, nil)
	if reverse.Shared == nil || *got.Shared != *reverse.Shared {
		t.Fatalf("reversed rider order changes the result: %+v vs %+v", got.Shared, reverse.Shared)
	}
}

func TestCompareRideTracksBriefInterruption(t *testing.T) {
	a := proximityTrack(0, 316, 1, func(t float64) (float64, float64) { return 8 * t, 0 })
	b := proximityTrack(0, 316, 1, func(t float64) (float64, float64) { return 8 * t, 12 })
	b = append(b[:151:151], b[167:]...)
	// 316 seconds less a 17-second gap is just short of five minutes.
	if got := CompareRideTracks(a, b, nil, nil).Shared; got != nil {
		t.Fatalf("gap contributed to the shared-duration threshold: %+v", got)
	}
	a = proximityTrack(0, 320, 1, func(t float64) (float64, float64) { return 8 * t, 0 })
	b = proximityTrack(0, 320, 1, func(t float64) (float64, float64) { return 8 * t, 12 })
	b = append(b[:151:151], b[167:]...)
	got := CompareRideTracks(a, b, nil, nil).Shared
	if got == nil || got.Seconds != 303 || math.Abs(got.DistanceM-2424) > 1 {
		t.Fatalf("brief interruption split the ride or added unobserved distance: %+v", got)
	}
}

func TestCompareRideTracksCrossedPaths(t *testing.T) {
	a := proximityTrack(0, 120, 1, func(t float64) (float64, float64) { return (t - 60) * 7, 0 })
	b := proximityTrack(2, 122, 4, func(t float64) (float64, float64) { return (60 - t) * 6, 5 })
	got := CompareRideTracks(a, b, nil, nil)
	if got.Shared != nil || len(got.Encounters) != 1 {
		t.Fatalf("expected one opposing-direction encounter: %+v", got)
	}
	event := got.Encounters[0]
	if math.Abs(event.Time.Sub(proximityTestStart.Add(time.Minute)).Seconds()) > .1 ||
		HaversineMeters(event.Location, GeoPoint{Lat: 51, Lon: 4}) > 4 {
		t.Fatalf("incorrect closest time or location: %+v", event)
	}
	zone := PrivacyZone{Lat: 51, Lng: 4, RadiusM: 20}
	if got := CompareRideTracks(a, b, []PrivacyZone{zone}, nil); got.Shared != nil || len(got.Encounters) > 0 {
		t.Fatalf("private location affected the result: %+v", got)
	}
}

func TestCompareRideTracksRejectsUnreliableEncounter(t *testing.T) {
	a := proximityTrack(0, 120, 1, func(t float64) (float64, float64) { return (t - 60) * 7, 0 })
	crossing := func(t float64) (float64, float64) { return (60 - t) * 6, 5 }
	gapped := slices.DeleteFunc(proximityTrack(0, 120, 1, crossing), func(s StreamSample) bool {
		return math.Abs(s.Timestamp.Sub(proximityTestStart).Seconds()-60) < 15
	})
	delayed := proximityTrack(0, 120, 1, crossing)
	for i := range delayed {
		delayed[i].Timestamp = delayed[i].Timestamp.Add(24 * time.Hour)
	}
	for _, tc := range []struct {
		name  string
		track []StreamSample
	}{
		{"stationary", proximityTrack(0, 120, 1, func(float64) (float64, float64) { return 0, 0 })},
		{"missing", nil},
		{"recording gap", gapped},
		{"different dates", delayed},
		{"parallel over 50m", proximityTrack(0, 120, 1, func(t float64) (float64, float64) { return (t - 60) * 7, 55 })},
		{"sustained under five minutes", proximityTrack(0, 120, 1, func(t float64) (float64, float64) { return (t - 60) * 7, 10 })},
		{"impossible jump", proximityTrack(0, 120, 1, func(t float64) (float64, float64) {
			if t == 60 {
				return 0, 0
			}
			return (t - 60) * 7, 1000
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompareRideTracks(a, tc.track, nil, nil); got.Shared != nil || len(got.Encounters) > 0 {
				t.Fatalf("unreliable track caused a match: %+v", got)
			}
		})
	}
}
