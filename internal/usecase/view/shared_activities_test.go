package view

import (
	"testing"
	"time"
)

func TestSharedActivitiesDeduplicateRecordings(t *testing.T) {
	start := time.Date(2025, 8, 1, 8, 0, 0, 0, time.UTC)
	later := start.Add(time.Hour)
	out := SharedActivities{
		Rides: []SharedRide{
			{Date: start, end: start.Add(8 * time.Minute), DistanceM: 3200, DurationS: 480},
			{Date: start.Add(time.Second), end: start.Add(10 * time.Minute), DistanceM: 4000, DurationS: 599},
		},
		Encounters: []CrossedPath{
			{Time: later.Add(time.Second), Latitude: 51, Longitude: 4.00001},
			{Time: start.Add(time.Minute), Latitude: 51, Longitude: 4},
			{Time: later, Latitude: 51, Longitude: 4},
			{Time: later.Add(time.Minute), Latitude: 51.01, Longitude: 4},
		},
	}
	out.deduplicate(time.Time{}, time.Time{})
	if len(out.Rides) != 1 || out.SharedDistanceM != 4000 || out.Rides[0].DurationS != 599 {
		t.Fatalf("duplicate rides inflated the result: %+v", out)
	}
	if len(out.Encounters) != 2 || !out.Encounters[0].Time.Equal(later) ||
		!out.Encounters[1].Time.Equal(later.Add(time.Minute)) {
		t.Fatalf("encounters were duplicated, lost or out of order: %+v", out.Encounters)
	}
}
