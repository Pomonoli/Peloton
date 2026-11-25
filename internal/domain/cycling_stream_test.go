package domain

import (
	"math"
	"testing"
	"time"
)

func cyclingTestSample(seconds, metres float64) StreamSample {
	return StreamSample{
		Timestamp: time.Unix(1700000000, int64(seconds*1e9)),
		Latitude:  new(0.0), Longitude: new(12 + metres*180/(math.Pi*EarthRadiusMeters)),
		DistanceM: new(metres),
	}
}

func TestValidateCyclingRejectsJumpWithoutNeighbouringSamples(t *testing.T) {
	result := ValidateCyclingStream([]StreamSample{cyclingTestSample(0, 0), cyclingTestSample(10, 400)})
	if result.Legs[1].Valid {
		t.Fatal("accepted 400 m in 10 seconds without corroborating movement")
	}
}

func TestValidateCyclingRejectsBrokenIntervals(t *testing.T) {
	base := []StreamSample{cyclingTestSample(0, 0), cyclingTestSample(1, 7), cyclingTestSample(2, 14),
		cyclingTestSample(3, 21), cyclingTestSample(4, 28), cyclingTestSample(5, 35), cyclingTestSample(6, 42),
		cyclingTestSample(7, 49), cyclingTestSample(8, 56)}
	for _, tc := range []struct {
		name   string
		adjust func([]StreamSample)
	}{
		{"400 m displacement in ten seconds", func(s []StreamSample) { s[4] = cyclingTestSample(13, 421) }},
		{"moderate speed but implausible acceleration", func(s []StreamSample) {
			s[4] = cyclingTestSample(4, 46)
			s[5] = cyclingTestSample(5, 53)
			s[6] = cyclingTestSample(6, 60)
			s[7] = cyclingTestSample(7, 67)
			s[8] = cyclingTestSample(8, 74)
		}},
		{"backwards clock", func(s []StreamSample) { s[4].Timestamp = s[2].Timestamp }},
		{"invalid timestamp", func(s []StreamSample) { s[4].Timestamp = time.Time{} }},
		{"missing original samples", func(s []StreamSample) { s[3].RecordingIndex, s[4].RecordingIndex = 7, 9 }},
		{"missing GPS fix", func(s []StreamSample) { s[4].Longitude = nil }},
		{"GPS signal time gap", func(s []StreamSample) {
			for i := 4; i < len(s); i++ {
				s[i] = cyclingTestSample(float64(i+9), float64(i*7+63))
			}
		}},
		{"distance inflated at a plausible GPS fix", func(s []StreamSample) { s[4].DistanceM = new(42.0) }},
		{"distance reset", func(s []StreamSample) { s[4].DistanceM = new(1.0) }},
		{"nonfinite distance", func(s []StreamSample) { s[4].DistanceM = new(math.Inf(1)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples := append([]StreamSample(nil), base...)
			tc.adjust(samples)
			result := ValidateCyclingStream(samples)
			if result.Legs[4].Valid {
				t.Fatalf("accepted invalid interval: %+v", result.Legs[4])
			}
			if !result.Legs[2].Valid || !result.Legs[8].Valid {
				t.Fatal("lost valid riding on either side of the anomaly")
			}
		})
	}
}

func TestValidateCyclingKeepsSustainedDescentAndRejectsSpike(t *testing.T) {
	samples := make([]StreamSample, 150)
	for i := range samples {
		samples[i] = cyclingTestSample(float64(2*i), float64(58*i)) // 104.4 km/h
		samples[i].SpeedMps = new(29.0)
	}
	// Corrupt device speed alone must not erase a corroborated GPS leg or
	// determine peak speed.
	samples[25].SpeedMps = new(99.0)
	validated := ValidateCyclingStream(samples)
	distance, moving, peak := validated.SpeedSummary()
	if math.Abs(distance-58*149) > 0.01 || moving != 298 || math.Abs(peak-29) > 0.01 {
		t.Fatalf("downhill distance=%v time=%v peak=%v", distance, moving, peak)
	}

	spike := []StreamSample{cyclingTestSample(0, 0), cyclingTestSample(1, 5), cyclingTestSample(2, 30),
		cyclingTestSample(3, 6), cyclingTestSample(4, 11)}
	for i := range spike {
		spike[i].DistanceM = nil
	}
	result := ValidateCyclingStream(spike)
	if result.Legs[2].Valid || result.Legs[3].Valid || !result.Legs[1].Valid || !result.Legs[4].Valid {
		t.Fatalf("spike boundaries: %+v", result.Legs)
	}
}

func TestValidateCyclingSuppressesStationaryGPSDrift(t *testing.T) {
	samples := make([]StreamSample, 180)
	for i := range samples {
		samples[i] = cyclingTestSample(float64(i), float64(i%2)*4)
		samples[i].DistanceM = new(float64(i) * 4) // a GPS-derived odometer accumulates jitter
	}
	distance, moving, peak := ValidateCyclingStream(samples).SpeedSummary()
	if distance != 0 || moving != 0 || peak != 0 || *samples[len(samples)-1].DistanceM != 716 {
		t.Fatalf("stationary drift leaked into summary or raw data changed: %v, %v, %v", distance, moving, peak)
	}
}

func TestCyclingActivityDistanceFallbacks(t *testing.T) {
	sparse := []StreamSample{
		cyclingTestSample(0, 0), cyclingTestSample(1, 10), cyclingTestSample(3, 30),
		cyclingTestSample(6, 60), cyclingTestSample(8, 80), cyclingTestSample(10, 100),
		cyclingTestSample(17, 170), cyclingTestSample(25, 250), cyclingTestSample(34, 340),
	}
	motion := ValidateCyclingStream(sparse)
	for _, tc := range []struct {
		name  string
		total *float64
		want  float64
	}{
		{"corroborated source", new(342.0), 342},
		{"missing", nil, 100},
		{"zero", new(0.0), 100},
		{"negative", new(-1.0), 100},
		{"nonfinite", new(math.Inf(1)), 100},
		{"NaN", new(math.NaN()), 100},
		{"inflated", new(1000.0), 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CyclingActivityDistance(sparse, motion, tc.total); got == nil || *got != tc.want {
				t.Fatalf("distance = %v, want %v", Deref(got), tc.want)
			}
		})
	}
	for _, adjust := range []func([]StreamSample){
		func(s []StreamSample) {
			// An ordinality break also excludes the interval from total corroboration.
			s[5].RecordingIndex, s[6].RecordingIndex = 6, 8
		},
		func(s []StreamSample) {
			// A large GPS jump remains invalid even inside the sparse sampling range.
			s[6].Longitude = new(13.0)
		},
	} {
		broken := append([]StreamSample(nil), sparse...)
		adjust(broken)
		if got := CyclingActivityDistance(broken, ValidateCyclingStream(broken), new(340.0)); got == nil || *got != 100 {
			t.Fatalf("broken recording distance = %v, want 100", Deref(got))
		}
	}
	if got := CyclingActivityDistance(nil, CyclingStream{}, new(340.0)); Deref(got) != 340 {
		t.Fatalf("summary-only distance = %v, want 340", Deref(got))
	}
	if got := CyclingActivityDistance(nil, CyclingStream{}, new(math.Inf(1))); got != nil {
		t.Fatalf("invalid summary-only distance = %v, want nil", *got)
	}
}
