package besteffort

import (
	"math"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

func TestCyclingEffortsRejectJumpsAndKeepFastestMeasuredWindow(t *testing.T) {
	start := time.Unix(1700000000, 0)
	samples := make([]domain.StreamSample, 0, 110)
	appendPoint := func(seconds int, metres float64) {
		samples = append(samples, domain.StreamSample{
			Timestamp: start.Add(time.Duration(seconds) * time.Second), DistanceM: new(metres),
			Latitude: new(0.0), Longitude: new(12 + metres*180/(math.Pi*domain.EarthRadiusMeters)),
		})
	}
	for i := 0; i <= 40; i += 2 {
		appendPoint(i, float64(i*8))
	}
	// The pre- and post-jump blocks each have real records. Combining them
	// across the corrupt interval would create a faster result.
	for i := 50; i <= 200; i += 3 {
		appendPoint(i, 720+float64(i-50)*10)
	}
	act := domain.Activity{Type: domain.ActivityTypeRide, StartTime: start}
	uc := NewComputeBestEffortsForSource(nil, nil, nil, nil, nil, nil)
	windows := []windowSpec{
		{domain.BestEffortMetricPace, domain.BestEffortWindowDistance, 400},
		{domain.BestEffortMetricPace, domain.BestEffortWindowDistance, 1609},
		{domain.BestEffortMetricSpeed, domain.BestEffortWindowDuration, 60},
	}
	got := uc.computeAll(act, domain.ActivitySource{}, samples, windows)
	if len(got) != 2 {
		t.Fatalf("expected one pace and one speed result, got %+v", got)
	}
	if pace := got[0]; pace.DurationS != 40 || *pace.DistanceM != 400 || pace.AchievedValue != 100 || pace.StartOffset < 50 {
		t.Fatalf("400 m effort: %+v", pace)
	}
	if speed := got[1]; speed.DurationS != 60 || speed.AchievedValue != 10 || speed.StartOffset < 50 {
		t.Fatalf("60 second effort: %+v", speed)
	}
}

func TestFastestCyclingWindowConsidersEndingOnSample(t *testing.T) {
	points := []cyclingPoint{{0, 0}, {8, 40}, {12, 80}}
	offset, elapsed, distance := fastestCyclingWindow(points, 50, true)
	if offset != 6 || elapsed != 6 || distance != 50 {
		t.Fatalf("distance window: %v, %v, %v", offset, elapsed, distance)
	}
	offset, elapsed, distance = fastestCyclingWindow(points, 5, false)
	if offset != 7 || elapsed != 5 || distance != 45 {
		t.Fatalf("time window: %v, %v, %v", offset, elapsed, distance)
	}
}

func TestFilteredCyclingRecordsChooseSlowerWindowsInSameRecording(t *testing.T) {
	start := time.Unix(1700000000, 0)
	var samples []domain.StreamSample
	for i := range 13 {
		seconds := i * 10
		distance := 15*float64(min(seconds, 60)) + 8*float64(max(0, seconds-60))
		altitude := 200 - .08*15*float64(min(seconds, 60))
		samples = append(samples, domain.StreamSample{
			Timestamp: start.Add(time.Duration(seconds) * time.Second),
			Latitude:  new(51 + distance*180/(math.Pi*domain.EarthRadiusMeters)), Longitude: new(4.0),
			DistanceM: &distance, AltitudeM: &altitude,
		})
	}
	windows := []windowSpec{
		{domain.BestEffortMetricSpeed, domain.BestEffortWindowDuration, 30},
		{domain.BestEffortMetricPace, domain.BestEffortWindowDistance, 300},
	}
	uc := NewComputeBestEffortsForSource(nil, nil, nil, nil, nil, nil)
	act := domain.Activity{Type: domain.ActivityTypeRide, StartTime: start}
	got := uc.computeAll(act, domain.ActivitySource{}, samples, windows)
	if len(got) != 4 {
		t.Fatalf("expected both record variants per window: %+v", got)
	}
	if got[0].AchievedValue != 15 || got[1].DurationS != 20 {
		t.Fatalf("expected the descent to win before filtering: %+v", got[:2])
	}
	if !got[2].DownhillExcluded || !got[3].DownhillExcluded ||
		got[2].AchievedValue < 8 || got[2].AchievedValue >= 10 ||
		got[3].DurationS <= 30 || got[3].DurationS > 37.5 {
		t.Fatalf("expected a slower valid alternative for both categories: %+v", got[2:])
	}
}

func TestDownhillWindowConsidersEligibilityBoundaries(t *testing.T) {
	p := downhillProfile{
		points:    []cyclingPoint{{0, 0}, {10, 100}, {20, 160}},
		altitude:  []float64{20, 10, 10},
		assistedM: []float64{0, 0, 0},
	}
	offset, elapsed, distance := p.fastestWindow(10, false)
	if math.Abs(offset-7) > 1e-6 || elapsed != 10 || math.Abs(distance-72) > 1e-6 {
		t.Fatalf("interpolated eligible optimum: %g, %g, %g", offset, elapsed, distance)
	}
}

func TestShortFlatCyclingWindowCanBeVerified(t *testing.T) {
	start := time.Unix(1700000000, 0)
	samples := []domain.StreamSample{
		{Timestamp: start, Latitude: new(51.0), Longitude: new(4.0), AltitudeM: new(100.0)},
		{Timestamp: start.Add(5 * time.Second), Latitude: new(51.0002), Longitude: new(4.0), AltitudeM: new(100.0)},
	}
	uc := NewComputeBestEffortsForSource(nil, nil, nil, nil, nil, nil)
	w := windowSpec{domain.BestEffortMetricSpeed, domain.BestEffortWindowDuration, 5}
	got := uc.computeAll(domain.Activity{Type: domain.ActivityTypeRide}, domain.ActivitySource{}, samples, []windowSpec{w})
	if len(got) != 2 || !got[1].DownhillExcluded || got[1].AchievedValue != got[0].AchievedValue {
		t.Fatalf("expected a verified short flat effort: %+v", got)
	}
	*samples[1].AltitudeM = 95
	got = uc.computeAll(domain.Activity{Type: domain.ActivityTypeRide}, domain.ActivitySource{}, samples, []windowSpec{w})
	if len(got) != 1 || got[0].DownhillExcluded {
		t.Fatalf("short descent passed the filter: %+v", got)
	}
}
