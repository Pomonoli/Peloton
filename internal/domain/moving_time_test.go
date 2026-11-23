package domain

import (
	"testing"
	"time"
)

func TestEstimateGPSMovingTime(t *testing.T) {
	type section struct {
		seconds int
		mps     float64
	}
	for _, tc := range []struct {
		name         string
		sections     []section
		drift        bool
		change       func([]StreamSample) []StreamSample
		want, margin time.Duration
		ok           bool
	}{
		{name: "continuous", sections: []section{{180, 7.3}}, want: 180 * time.Second, ok: true},
		{name: "traffic lights", sections: []section{{40, 7}, {7, 0}, {60, 8}, {12, 0}, {45, 6}},
			want: 145 * time.Second, margin: time.Second, ok: true},
		{name: "long break", sections: []section{{65, 7}, {180, 0}, {85, 8}},
			want: 150 * time.Second, margin: time.Second, ok: true},
		{name: "slow climb after a stop", sections: []section{{25, 0}, {120, .85}},
			want: 120 * time.Second, margin: 5 * time.Second, ok: true},
		{name: "stationary drift", sections: []section{{45, 7}, {75, 0}, {45, 7}}, drift: true,
			want: 90 * time.Second, margin: 8 * time.Second, ok: true},
		{name: "GPS outlier", sections: []section{{180, 8}}, change: func(s []StreamSample) []StreamSample {
			s[50].Longitude = cyclingTestSample(50, 400+700).Longitude
			s[110].Longitude = cyclingTestSample(110, 880+24).Longitude
			return s
		}, want: 176 * time.Second, margin: 2 * time.Second, ok: true},
		{name: "irregular recording and a gap", sections: []section{{180, 7}}, change: func(s []StreamSample) []StreamSample {
			var sparse []StreamSample
			last, step := 0, 0
			for i, p := range s {
				if i > 60 && i < 93 {
					continue
				}
				if i == 0 || i-last >= []int{2, 5, 3, 8}[step%4] || i == len(s)-1 {
					sparse = append(sparse, p)
					last = i
					step++
				}
			}
			return sparse
		}, want: 146 * time.Second, margin: 5 * time.Second, ok: true},
		{name: "missing and repeated timestamps", sections: []section{{180, 7}}, change: func(s []StreamSample) []StreamSample {
			s[58].Timestamp = time.Time{}
			s[132].Timestamp = s[131].Timestamp
			return s
		}, want: 177 * time.Second, margin: 2 * time.Second, ok: true},
		{name: "incomplete track", sections: []section{{180, 7}}, change: func(s []StreamSample) []StreamSample {
			return s[:18]
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples := []StreamSample{cyclingTestSample(0, 0)}
			var seconds int
			var metres float64
			drift := []float64{0, 2, -1, 3, 1, -3, 2, 0, -2}
			for _, part := range tc.sections {
				for range part.seconds {
					seconds++
					metres += part.mps
					position := metres
					if part.mps == 0 && tc.drift {
						position += drift[seconds%len(drift)]
					}
					samples = append(samples, cyclingTestSample(float64(seconds), position))
				}
			}
			if tc.change != nil {
				samples = tc.change(samples)
			}
			got, ok := EstimateGPSMovingTime(samples, time.Duration(seconds)*time.Second, nil)
			if ok != tc.ok || got < tc.want-tc.margin || got > tc.want+tc.margin {
				t.Fatalf("got %s (%t), want %s ± %s (%t)", got, ok, tc.want, tc.margin, tc.ok)
			}
		})
	}
}
