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
