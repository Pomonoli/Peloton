package besteffort

import (
	"math"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

func TestClimbingRecordsUseBestContinuousWindow(t *testing.T) {
	start := time.Unix(1700000000, 0)
	climb := func(seconds, begin, end, gain float64) float64 {
		return gain * max(0, min(1, (seconds-begin)/(end-begin)))
	}
	firstClimb := func(s float64) float64 {
		return 100 + climb(s, 600, 1500, 400) - climb(s, 1500, 2400, 350)
	}
	for _, tc := range []struct {
		name     string
		elapsed  int
		cadence  []int
		altitude func(float64) float64
		want     float64
	}{
		{"climb and descent", 3600, []int{1}, firstClimb, 400},
		{"multiple climbs", 4500, []int{5}, func(s float64) float64 {
			return firstClimb(s) + climb(s, 2400, 3300, 200)
		}, 600},
		{"flat with noise", 4320, []int{1}, func(s float64) float64 {
			return 100 + 1.2*math.Sin(s/2)
		}, 0},
		{"smart recording", 4320, []int{4, 8, 18, 25}, func(s float64) float64 { return s / 10 }, 360},
		{"best hour excludes early climb", 4800, []int{5}, func(s float64) float64 {
			return climb(s, 0, 600, 100) + climb(s, 4200, 4800, 300)
		}, 300},
		{"short recording", 3595, []int{5}, firstClimb, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var samples []domain.StreamSample
			for i, seconds := 0, 0; seconds <= tc.elapsed; i++ {
				samples = append(samples, domain.StreamSample{
					Timestamp: start.Add(time.Duration(seconds) * time.Second), RecordingIndex: int32(i + 1),
					AltitudeM: new(tc.altitude(float64(seconds))),
				})
				seconds += tc.cadence[i%len(tc.cadence)]
			}
			uc := NewComputeBestEffortsForSource(nil, nil, nil, nil, nil, nil)
			w := windowSpec{domain.BestEffortMetricVAM, domain.BestEffortWindowDuration, 3600}
			got := uc.computeAll(domain.Activity{Type: domain.ActivityTypeRide}, domain.ActivitySource{}, samples, []windowSpec{w})
			if tc.want == 0 {
				if len(got) != 0 {
					t.Fatalf("unexpected climbing result: %+v", got)
				}
			} else if len(got) != 1 || got[0].DurationS != 3600 || math.Abs(got[0].AchievedValue-tc.want) > 1e-6 {
				t.Fatalf("got %+v, want %g m in an hour", got, tc.want)
			}
		})
	}
}

func TestClimbingRecordsDoNotBridgeRecordingBreaks(t *testing.T) {
	start := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name           string
		breakRecording func([]domain.StreamSample)
	}{
		{"missing altitude", func(s []domain.StreamSample) { s[200].AltitudeM = nil }},
		{"invalid altitude", func(s []domain.StreamSample) { s[200].AltitudeM = new(math.NaN()) }},
		{"implausible jump", func(s []domain.StreamSample) { s[200].AltitudeM = new(500.0) }},
		{"duplicate time", func(s []domain.StreamSample) { s[200].Timestamp = s[199].Timestamp }},
		{"missing time", func(s []domain.StreamSample) { s[200].Timestamp = time.Time{} }},
		{"missing sample", func(s []domain.StreamSample) { s[200].RecordingIndex = s[199].RecordingIndex + 2 }},
		{"gap in a dense recording", func(s []domain.StreamSample) {
			for i := 200; i < len(s); i++ {
				s[i].Timestamp = s[i].Timestamp.Add(20 * time.Second)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples := make([]domain.StreamSample, 401)
			for i := range samples {
				samples[i] = domain.StreamSample{
					Timestamp: start.Add(time.Duration(i) * time.Second), RecordingIndex: int32(i + 1),
					AltitudeM: new(float64(i) / 5),
				}
			}
			tc.breakRecording(samples)
			uc := NewComputeBestEffortsForSource(nil, nil, nil, nil, nil, nil)
			w := windowSpec{domain.BestEffortMetricVAM, domain.BestEffortWindowDuration, 300}
			if got, ok := uc.timedSensorEffort(domain.Activity{}, domain.ActivitySource{}, samples, w, start); ok {
				t.Fatalf("effort crossed invalid interval: %+v", got)
			}
		})
	}
}
