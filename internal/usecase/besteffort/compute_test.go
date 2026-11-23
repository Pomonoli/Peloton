package besteffort

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

func TestTimedSensorEffortUsesTimestampsAndRejectsGaps(t *testing.T) {
	start := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	samples := []domain.StreamSample{
		{Timestamp: start, PowerW: new(int16(100))},
		{Timestamp: start.Add(3 * time.Second), PowerW: new(int16(100))},
		{Timestamp: start.Add(6 * time.Second), PowerW: new(int16(100))},
		{Timestamp: start.Add(9 * time.Second)},
		{Timestamp: start.Add(20 * time.Second), PowerW: new(int16(900))},
		{Timestamp: start.Add(50 * time.Second), PowerW: new(int16(900))},
	}
	uc := NewComputeBestEffortsForSource(nil, nil, nil, nil, nil, nil)
	window := windowSpec{domain.BestEffortMetricPower, domain.BestEffortWindowDuration, 5}
	result, ok := uc.timedSensorEffort(domain.Activity{}, domain.ActivitySource{}, samples, window, start)
	if !ok || result.AchievedValue != 100 || result.DurationS != 5 {
		t.Fatalf("incorrect measured window: %+v, %v", result, ok)
	}
	window.Value = 10
	if _, ok := uc.timedSensorEffort(domain.Activity{}, domain.ActivitySource{}, samples, window, start); ok {
		t.Fatal("missing data was bridged to create a record")
	}
}

// ---------------------------------------------------------------------------
// slidingWindowMinPace
// ---------------------------------------------------------------------------

// makeUniformPaceSamples builds N samples spaced 1 second apart, each
// `metresPerStep` ahead of the previous. Result has constant pace of
// (1 / metresPerStep) * 1000 seconds per km.
func makeUniformPaceSamples(n int, metresPerStep float64) []domain.StreamSample {
	out := make([]domain.StreamSample, n)
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		dist := float64(i) * metresPerStep
		out[i] = domain.StreamSample{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			DistanceM: &dist,
		}
	}
	return out
}

func TestSlidingWindowMinPace_UniformPace(t *testing.T) {
	// 100 samples, 4 m/s pace (4 m per step, 1 step per second) → pace = 250 s/km.
	samples := makeUniformPaceSamples(100, 4)
	pace, startIdx, endIdx, dist, ok := slidingWindowMinPace(samples, 100)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// 100m at 4 m/s = 25 seconds elapsed. 25s / (100m / 1000) = 250 s/km.
	if math.Abs(pace-250) > 1 {
		t.Errorf("pace = %f s/km, want 250 ± 1", pace)
	}
	if endIdx-startIdx < 25 || endIdx-startIdx > 26 {
		t.Errorf("window length = %d samples, want 25 or 26", endIdx-startIdx)
	}
	if dist < 100 {
		t.Errorf("dist = %f, must be >= 100", dist)
	}
}

func TestSlidingWindowMinPace_BelowDistance(t *testing.T) {
	// Only 50m of distance — can't fit a 100m window.
	samples := makeUniformPaceSamples(50, 1)
	_, _, _, _, ok := slidingWindowMinPace(samples, 100)
	if ok {
		t.Error("expected ok=false: total distance below window")
	}
}

func TestSlidingWindowMinPace_PicksFastestSegment(t *testing.T) {
	// 1km of uniform 4 m/s, then a 200m surge at 8 m/s, then more 4 m/s.
	// The fastest 200m should be entirely in the surge with pace = 125 s/km.
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	var samples []domain.StreamSample
	var dist float64
	ts := base

	// 250s of slow (250 samples × 4 m = 1000 m).
	for i := 0; i < 250; i++ {
		d := dist
		samples = append(samples, domain.StreamSample{Timestamp: ts, DistanceM: &d})
		dist += 4
		ts = ts.Add(time.Second)
	}
	// 25s of fast (25 samples × 8 m = 200 m).
	for i := 0; i < 25; i++ {
		d := dist
		samples = append(samples, domain.StreamSample{Timestamp: ts, DistanceM: &d})
		dist += 8
		ts = ts.Add(time.Second)
	}
	// 250s more slow.
	for i := 0; i < 250; i++ {
		d := dist
		samples = append(samples, domain.StreamSample{Timestamp: ts, DistanceM: &d})
		dist += 4
		ts = ts.Add(time.Second)
	}

	pace, startIdx, _, _, ok := slidingWindowMinPace(samples, 200)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// The fastest 200m window is the surge: 25 seconds for 200m → 125 s/km.
	if math.Abs(pace-125) > 2 {
		t.Errorf("pace = %f s/km, want 125 ± 2", pace)
	}
	// Surge starts at index 250.
	if startIdx < 245 || startIdx > 255 {
		t.Errorf("startIdx = %d, want around 250", startIdx)
	}
}

func TestComputeVAMEffort(t *testing.T) {
	fixedNow := func() time.Time { return time.Unix(0, 0).UTC() }
	fixedID := func() uuid.UUID { return uuid.Nil }
	uc := NewComputeBestEffortsForSource(nil, nil, nil, nil, fixedNow, fixedID)

	// 400 samples at 1Hz, climbing 1 m/sample. Over a 300s window the gain is
	// 300 m → VAM = 300 / (300/3600) h = 3600 m/h.
	base := time.Unix(1000, 0).UTC()
	samples := make([]domain.StreamSample, 400)
	for i := range samples {
		alt := float64(i)
		samples[i] = domain.StreamSample{Timestamp: base.Add(time.Duration(i) * time.Second), AltitudeM: &alt}
	}
	w := windowSpec{domain.BestEffortMetricVAM, domain.BestEffortWindowDuration, 300}
	be, ok := uc.timedSensorEffort(domain.Activity{}, domain.ActivitySource{}, samples, w, fixedNow())
	if !ok {
		t.Fatal("expected a VAM effort")
	}
	if be.Metric != domain.BestEffortMetricVAM {
		t.Fatalf("metric = %q", be.Metric)
	}
	if math.Abs(be.AchievedValue-3600) > .01 {
		t.Fatalf("VAM = %.1f m/h, want 3600", be.AchievedValue)
	}

	// Flat altitude → no climbing window → no effort.
	flat := make([]domain.StreamSample, 400)
	for i := range flat {
		alt := 100.0
		flat[i] = domain.StreamSample{Timestamp: base.Add(time.Duration(i) * time.Second), AltitudeM: &alt}
	}
	if _, ok := uc.timedSensorEffort(domain.Activity{}, domain.ActivitySource{}, flat, w, fixedNow()); ok {
		t.Fatal("flat altitude should produce no VAM effort")
	}
}

func TestReadMetricRejectsInvalidSensorSamples(t *testing.T) {
	sample := domain.StreamSample{PowerW: new(int16(6000)), HeartRateBpm: new(int16(320)), SpeedMps: new(math.NaN())}
	for _, metric := range []domain.BestEffortMetric{domain.BestEffortMetricPower, domain.BestEffortMetricHeartRate, domain.BestEffortMetricSpeed} {
		if _, ok := readMetric(sample, metric); ok {
			t.Errorf("%s accepted corrupt sample", metric)
		}
	}
	if value, ok := readMetric(domain.StreamSample{PowerW: new(int16(0))}, domain.BestEffortMetricPower); !ok || value != 0 {
		t.Fatal("a recorded zero watt sample is valid coasting data")
	}
}

func TestGPSOnlyHistoricalRunningRecords(t *testing.T) {
	start := time.Date(2024, 5, 1, 9, 0, 0, 0, time.UTC)
	samples := make([]domain.StreamSample, 310)
	for i := range samples {
		samples[i] = domain.StreamSample{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Latitude:  new(51 + float64(i*4)*180/(math.Pi*domain.EarthRadiusMeters)), Longitude: new(4.0),
		}
	}
	windows := []windowSpec{
		{domain.BestEffortMetricPace, domain.BestEffortWindowDistance, 1000},
		{domain.BestEffortMetricSpeed, domain.BestEffortWindowDuration, 60},
		{domain.BestEffortMetricHeartRate, domain.BestEffortWindowDuration, 60},
	}
	uc := NewComputeBestEffortsForSource(nil, nil, nil, nil, nil, nil)
	got := uc.computeAll(domain.Activity{Type: domain.ActivityTypeRun, StartTime: start}, domain.ActivitySource{}, samples, windows)
	if len(got) != 2 || math.Abs(got[0].AchievedValue-250) > .1 || math.Abs(got[1].AchievedValue-4) > .01 {
		t.Fatalf("GPS-only records: %+v", got)
	}
}
