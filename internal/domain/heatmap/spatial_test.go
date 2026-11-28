package heatmap

import (
	"math"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

func sample(x, y, seconds float64, hr, power *int16) domain.StreamSample {
	point := (Point{x, y}).Location()
	return domain.StreamSample{Latitude: &point.Lat, Longitude: &point.Lon, Timestamp: time.Unix(0, int64(seconds*1e9)),
		HeartRateBpm: hr, PowerW: power}
}

func TestPassageWeightsAcrossRides(t *testing.T) {
	fast := Aggregate([]domain.StreamSample{sample(3, 3, 0, new(int16(180)), new(int16(250))), sample(30, 3, 3, new(int16(180)), new(int16(250)))})
	slow := Aggregate([]domain.StreamSample{sample(3, 3, 0, new(int16(120)), nil), sample(11, 3, 2, new(int16(120)), nil), sample(30, 3, 9, new(int16(120)), nil)})
	if len(fast[Detail]) != 1 || len(slow[Detail]) != 1 {
		t.Fatalf("unexpected bins: %v %v", fast[Detail], slow[Detail])
	}
	got := fast[Detail][0]
	got.Merge(slow[Detail][0])
	if got.Passages != 2 {
		t.Fatalf("passages = %d", got.Passages)
	}
	assertNear(t, "distance / moving time", got.Distance/got.Moving, 4.5, 0.02)
	assertNear(t, "heart rate", *got.HeartRate.Value(), 135, 0.01)
	assertNear(t, "power with missing samples", *got.Power.Value(), 250, 0.01)
	if got.Elevation.Value() != nil || got.Gradient.Value() != nil {
		t.Fatal("missing measurements became values")
	}
	coast := Aggregate([]domain.StreamSample{sample(3, 3, 0, nil, new(int16(0))), sample(30, 3, 3, nil, new(int16(0)))})
	if v := coast[Detail][0].Power.Value(); v == nil || *v != 0 {
		t.Fatalf("recorded coasting: %v", v)
	}
}

func TestCadenceUsesMovingTimeAndPreservesRecordedZero(t *testing.T) {
	ride := func(seconds float64, rpm *int16) Bin {
		samples := []domain.StreamSample{sample(3, 3, 0, nil, nil), sample(30, 3, seconds, nil, nil)}
		samples[0].Cadence, samples[1].Cadence = rpm, rpm
		return Aggregate(samples)[Detail][0]
	}
	missing := ride(3, nil)
	if missing.Cadence.Value() != nil || ride(3, new(int16(300))).Cadence.Weight != 0 {
		t.Fatal("missing or invalid cadence became a value")
	}
	coasting := ride(3, new(int16(0)))
	if rpm := coasting.Cadence.Value(); rpm == nil || *rpm != 0 {
		t.Fatalf("coasting cadence = %v", rpm)
	}
	coasting.Merge(ride(9, new(int16(100))))
	coasting.Merge(missing)
	assertNear(t, "time-weighted cadence", *coasting.Cadence.Value(), 75, 0.01)
}

func TestRepeatedTraversalAndParallelRoad(t *testing.T) {
	route := []domain.StreamSample{sample(3, 3, 0, nil, nil), sample(32, 3, 4, nil, nil), sample(65, 3, 8, nil, nil),
		sample(32, 3, 12, nil, nil), sample(3, 3, 16, nil, nil)}
	out := Aggregate(route)[Detail]
	if len(out) != 2 || out[0].Passages != 2 || out[1].Passages != 2 {
		t.Fatalf("round trip: %+v", out)
	}
	parallel := Aggregate([]domain.StreamSample{sample(3, 15, 0, nil, nil), sample(32, 15, 4, nil, nil)})[Detail]
	if len(parallel) != 1 || parallel[0].Key == out[0].Key {
		t.Fatalf("parallel road merged into original: %+v", parallel)
	}
}

func TestRejectsGPSJumpsAndSmoothsElevation(t *testing.T) {
	samples := []domain.StreamSample{sample(3, 3, 0, nil, nil), sample(13, 3, 2, nil, nil),
		sample(23, 3, 4, nil, nil), sample(33, 3, 6, nil, nil), sample(43, 3, 8, nil, nil), sample(53, 3, 10, nil, nil),
		sample(2800, 3, 11, new(int16(800)), new(int16(20000)))}
	for i := range samples {
		samples[i].AltitudeM = new(100 + float64(i))
	}
	samples[2].AltitudeM = new(6000.0)
	out := Aggregate(samples)[Overview]
	if len(out) != 1 {
		t.Fatalf("corrupted GPS point rendered: %+v", out)
	}
	bin := out[0]
	assertNear(t, "total travel", bin.Distance, 50, 0.1)
	if v := bin.Elevation.Value(); v == nil || *v < 100 || *v > 106 {
		t.Fatalf("altitude spike not smoothed: %+v", v)
	}
	if v := bin.Gradient.Value(); v == nil || *v < 1 || *v > 15 {
		t.Fatalf("implausible or missing gradient: %+v", bin.Gradient)
	}
	if bin.HeartRate.Value() != nil || bin.Power.Value() != nil {
		t.Fatal("corrupted sensors were included")
	}
}

func TestDatelineDoesNotCreateALongLine(t *testing.T) {
	ride := Aggregate([]domain.StreamSample{
		{Latitude: new(1.0), Longitude: new(179.9999), Timestamp: time.Unix(0, 0)},
		{Latitude: new(1.0), Longitude: new(-179.9999), Timestamp: time.Unix(5, 0)},
	})
	for _, bin := range ride[Detail] {
		line := bin.Line()
		if math.Abs(line[0].Lon-line[1].Lon) > 0.001 || math.Abs(line[0].Lon) < 179.9 {
			t.Fatalf("line spans the map: %+v", line)
		}
	}
	if len(ride[Detail]) < 2 {
		t.Fatalf("missing either side of dateline: %+v", ride[Detail])
	}
}

func assertNear(t *testing.T, name string, value, want, tolerance float64) {
	t.Helper()
	if math.Abs(value-want) > tolerance {
		t.Errorf("%s = %v, want %v", name, value, want)
	}
}

func TestRecordedGradientWhenAltitudeIsMissing(t *testing.T) {
	samples := []domain.StreamSample{sample(3, 3, 0, nil, nil), sample(13, 3, 2, nil, nil), sample(23, 3, 4, nil, nil), sample(33, 3, 6, nil, nil)}
	for i := range samples {
		samples[i].Grade = new(float32(7))
	}
	samples[2].Grade = new(float32(26))
	result := Aggregate(samples)[Detail]
	if len(result) != 1 || result[0].Gradient.Value() == nil {
		t.Fatal("recorded gradient missing")
	}
	assertNear(t, "smoothed recorded gradient", *result[0].Gradient.Value(), 7, 0.01)
}
