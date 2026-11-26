//go:build integration

package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func TestAPI_FriendsDirectory(t *testing.T) {
	env := newITestEnv(t)
	alice := env.user("directory-alice", domain.UserRoleUser)
	bob := env.user("directory-bob", domain.UserRoleUser)
	idle := env.user("directory-idle", domain.UserRoleUser)
	admin := env.user("directory-admin", domain.UserRoleAdmin)
	ctx := t.Context()
	if err := env.app.Users.UpdateProfile(ctx, bob.userID, port.ProfileUpdate{
		DisplayName: new("Bob Rider"), AvatarURL: new("https://example.test/bob.png"),
	}); err != nil {
		t.Fatal(err)
	}
	record := func(kind domain.ActivityType, distance float64, day int) string {
		t.Helper()
		start := time.Date(2026, 8, day, 10, 0, 0, 0, time.UTC)
		latitude := 49.0 + float64(day)
		act := domain.Activity{
			ID: domain.ActivityID(uuid.New()), UserID: bob.userID, Type: kind, Title: "Directory activity",
			StartTime: start, EndTime: start.Add(time.Hour), MovingDuration: time.Hour, ElapsedDuration: time.Hour,
			Timezone: "UTC", Privacy: domain.PrivacyPublic, Summary: domain.ActivitySummary{DistanceM: &distance},
		}
		if err := env.app.Activities.SaveActivity(ctx, act); err != nil {
			t.Fatal(err)
		}
		if err := env.app.Activities.SetStartLocation(ctx, act.ID, &latitude, new(4.0), "Recorded start"); err != nil {
			t.Fatal(err)
		}
		return act.ID.String()
	}
	maskedRide := record(domain.ActivityTypeRide, 12000, 1)
	lastRide := record(domain.ActivityTypeRide, 24000, 2)
	record(domain.ActivityTypeRun, 3000, 3)
	hidden := record(domain.ActivityTypeRide, 48000, 4)
	admin.post("/api/admin/activities/"+hidden+"/hide", map[string]any{"hidden": true}, http.StatusOK)
	deleted := record(domain.ActivityTypeRide, 96000, 5)
	deletedID, err := domain.ParseUUID[domain.ActivityID](deleted)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.app.Activities.SoftDeleteActivity(ctx, deletedID, time.Now()); err != nil {
		t.Fatal(err)
	}

	type memberCard struct {
		ID              string
		DisplayName     string               `json:"display_name"`
		AvatarURL       string               `json:"avatar_url"`
		ActivityCount   int                  `json:"activity_count"`
		CyclingDistance float64              `json:"cycling_distance_m"`
		LatestRide      *struct{ ID string } `json:"latest_ride"`
	}
	membersFor := func(c *itestClient) map[string]memberCard {
		t.Helper()
		body := c.do(http.MethodGet, "/api/friends", nil, http.StatusOK)
		for _, forbidden := range []string{"@example.com", "\"password\"", "\"status\"", "\"email\"", "\"role\""} {
			if strings.Contains(string(body), forbidden) {
				t.Fatalf("directory disclosed account-only data: %s", body)
			}
		}
		var data struct{ Members []memberCard }
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatal(err)
		}
		out := make(map[string]memberCard)
		for _, member := range data.Members {
			out[member.ID] = member
		}
		return out
	}
	env.anon().do(http.MethodGet, "/api/friends", nil, http.StatusUnauthorized)
	visible := membersFor(alice)
	friend := visible[bob.userID.String()]
	if len(visible) != 4 || friend.ActivityCount != 3 || friend.CyclingDistance != 36000 ||
		friend.LatestRide == nil || friend.LatestRide.ID != lastRide || friend.DisplayName != "Bob Rider" || friend.AvatarURL == "" {
		t.Fatalf("unexpected directory: %+v", visible)
	}
	if member := visible[idle.userID.String()]; member.ID == "" || member.ActivityCount != 0 || member.LatestRide != nil {
		t.Fatalf("member without rides: %+v", member)
	}
	if self := membersFor(bob)[bob.userID.String()]; self.ActivityCount != 4 || self.CyclingDistance != 84000 ||
		self.LatestRide == nil || self.LatestRide.ID != hidden {
		t.Fatalf("owner directory projection: %+v", self)
	}
	bob.post("/api/profile/privacy-zones", map[string]any{"lat": 50, "lng": 4, "radius_m": 200}, http.StatusOK)
	alice.post("/api/profile/privacy-zones", map[string]any{"lat": 51, "lng": 4, "radius_m": 200}, http.StatusOK)
	for _, client := range []*itestClient{alice, bob} {
		profile := client.do(http.MethodGet, "/api/profiles/directory-bob", nil, http.StatusOK)
		var data struct {
			Activities []struct {
				ID       string
				Redacted bool     `json:"start_location_redacted"`
				Latitude *float64 `json:"start_lat"`
				Place    string   `json:"start_place"`
			}
		}
		if err := json.Unmarshal(profile, &data); err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, ride := range data.Activities {
			seen[ride.ID] = true
			if ride.ID != maskedRide && ride.ID != lastRide {
				continue
			}
			mask := client == alice && ride.ID == maskedRide
			if ride.Redacted != mask || (ride.Latitude == nil) != mask || (ride.Place == "") != mask {
				t.Fatalf("%s saw incorrect location for ride %s: %+v", client.name, ride.ID, ride)
			}
		}
		if !seen[maskedRide] || !seen[lastRide] || seen[deleted] || seen[hidden] != (client == bob) {
			t.Fatalf("%s profile history diverged from rider visibility: %s", client.name, profile)
		}
	}
}

func TestAPI_MemberSharing(t *testing.T) {
	env := newITDiscoveryEnv(t)
	owner := env.user("visibility-owner", domain.UserRoleUser)
	viewer := env.user("visibility-viewer", domain.UserRoleUser)
	profilePath := "/api/profiles/" + owner.name
	ride := owner.upload("member-shared.gpx", gpxRide(time.Date(2021, 8, 1, 9, 0, 0, 0, time.UTC)))
	id := str(ride, "activity_id")
	aid, err := domain.ParseUUID[domain.ActivityID](id)
	if err != nil {
		t.Fatal(err)
	}
	activity, err := env.app.Activities.GetActivity(t.Context(), aid)
	if err != nil || activity.Privacy != domain.PrivacyPublic {
		t.Fatalf("new upload privacy = %s, err = %v", activity.Privacy, err)
	}
	laps := "/api/activities/" + id + "/laps"
	viewer.do(http.MethodGet, laps, nil, http.StatusOK)
	viewer.do(http.MethodGet, profilePath, nil, http.StatusOK)
	env.anon().do(http.MethodGet, profilePath, nil, http.StatusUnauthorized)
}
