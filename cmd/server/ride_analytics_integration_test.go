//go:build integration

package main

import (
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

func TestAPI_RideAnalyticsRawStreams(t *testing.T) {
	env := newITestEnv(t)
	rider := env.user("analytics-rider", domain.UserRoleUser)
	friend := env.user("analytics-friend", domain.UserRoleUser)
	hasChannel := func(channels []any, expected string) bool {
		t.Helper()
		for _, channel := range channels {
			if channel == expected {
				return true
			}
		}
		return false
	}

	for _, fixture := range []struct {
		name    string
		sensors bool
	}{
		{"sensor-ride.gpx", true},
		{"gps-only-ride.gpx", false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			start := time.Date(2024, 11, 15, 12, 0, 0, 0, time.UTC)
			if !fixture.sensors {
				start = start.Add(24 * time.Hour)
			}
			// Sub-second timestamps expose accidental use of the 5-second series.
			gpx := gpxRideAtInterval(start, 750*time.Millisecond)
			if !fixture.sensors {
				gpx = regexp.MustCompile(`<extensions>.*?</extensions>`).ReplaceAll(gpx, nil)
			}
			upload := rider.upload(fixture.name, gpx)
			body := friend.post("/cairn.v1.ActivityService/GetActivityStream", map[string]any{
				"activitySourceId": str(upload, "source_id"), "maxResolutionHz": 1,
			}, http.StatusOK)
			stream := body["stream"].(map[string]any)
			clock := stream["timeS"].([]any)
			if len(clock) != 1200 || clock[1] != 0.75 || clock[2] != 1.5 {
				t.Fatalf("raw replay clock has %d samples, first offsets %v", len(clock), clock[:min(3, len(clock))])
			}
			channels := stream["channels"].([]any)
			for _, channel := range []string{"STREAM_CHANNEL_LATITUDE", "STREAM_CHANNEL_LONGITUDE", "STREAM_CHANNEL_ALTITUDE"} {
				if !hasChannel(channels, channel) {
					t.Fatalf("missing channel %s: %v", channel, channels)
				}
			}
			for _, channel := range []string{"STREAM_CHANNEL_POWER", "STREAM_CHANNEL_HEART_RATE"} {
				if hasChannel(channels, channel) != fixture.sensors {
					t.Fatalf("unexpected %s availability: %v", channel, channels)
				}
			}
		})
	}
}
