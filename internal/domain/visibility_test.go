package domain

import "testing"

func TestPrivacyZone_Contains(t *testing.T) {
	// Zone centred on a point with a 200 m radius.
	z := PrivacyZone{Lat: 50.1000, Lng: 10.2000, RadiusM: 200}

	// The exact centre is inside.
	if !z.Contains(50.1000, 10.2000) {
		t.Fatal("centre should be inside the zone")
	}
	// ~50 m north (0.00045° lat ≈ 50 m) is inside.
	if !z.Contains(50.1000+0.00045, 10.2000) {
		t.Error("point ~50 m away should be inside the 200 m zone")
	}
	// ~1 km north is outside.
	if z.Contains(50.1000+0.009, 10.2000) {
		t.Error("point ~1 km away should be outside the 200 m zone")
	}
}

func TestPointInAnyZone(t *testing.T) {
	zones := []PrivacyZone{
		{Lat: 50.1000, Lng: 10.2000, RadiusM: 150}, // zone A
		{Lat: 50.5000, Lng: 10.8000, RadiusM: 300}, // zone B
	}

	// Inside the second zone.
	if !PointInAnyZone(zones, 50.5001, 10.8001) {
		t.Error("point in the second zone should match")
	}
	// Far from both.
	if PointInAnyZone(zones, 52.5200, 13.4050) {
		t.Error("a distant point is in neither zone")
	}
	// Empty zone list never matches (the common no-zones-configured case).
	if PointInAnyZone(nil, 50.1000, 10.2000) {
		t.Error("nil zones must never match")
	}
}
