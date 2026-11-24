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
		DisplayName: "Bob Rider", AvatarURL: "https://example.test/bob.png", Locale: "en", Timezone: "UTC", Units: "metric",
	}); err != nil {
		t.Fatal(err)
	}
	record := func(kind domain.ActivityType, distance float64, day int) string {
		t.Helper()
		start := time.Date(2026, 8, day, 10, 0, 0, 0, time.UTC)
		act := domain.Activity{
			ID: domain.ActivityID(uuid.New()), UserID: bob.userID, Type: kind, Title: "Directory activity",
			StartTime: start, EndTime: start.Add(time.Hour), MovingDuration: time.Hour, ElapsedDuration: time.Hour,
			Timezone: "UTC", Privacy: domain.PrivacyPublic, Summary: domain.ActivitySummary{DistanceM: &distance},
		}
		if err := env.app.Activities.SaveActivity(ctx, act); err != nil {
			t.Fatal(err)
		}
		return act.ID.String()
	}
	record(domain.ActivityTypeRide, 12000, 1)
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
	profile := string(alice.do(http.MethodGet, "/api/profiles/directory-bob", nil, http.StatusOK))
	if !strings.Contains(profile, lastRide) || strings.Contains(profile, hidden) || strings.Contains(profile, deleted) {
		t.Fatalf("profile history diverged from Friends visibility: %s", profile)
	}
}

func TestAPI_SharingDefaultsAndExplicitLimits(t *testing.T) {
	env := newITDiscoveryEnv(t)
	owner := env.user("visibility-owner", domain.UserRoleUser)
	viewer := env.user("visibility-viewer", domain.UserRoleUser)
	profilePath := "/api/profiles/" + owner.name
	var profile struct {
		Public bool `json:"is_public"`
	}
	if err := json.Unmarshal(viewer.do(http.MethodGet, profilePath, nil, http.StatusOK), &profile); err != nil || !profile.Public {
		t.Fatalf("new account profile = %+v, err = %v", profile, err)
	}

	var visibility struct {
		Policy     map[string][]string `json:"policy"`
		Categories []string            `json:"all_categories"`
		Audiences  []string            `json:"all_audiences"`
	}
	const policyPath = "/api/profile/visibility"
	if err := json.Unmarshal(owner.do(http.MethodGet, policyPath, nil, http.StatusOK), &visibility); err != nil {
		t.Fatal(err)
	}
	if len(visibility.Categories) != len(domain.AllCategories) || len(visibility.Audiences) != 3 {
		t.Fatalf("unexpected settings options: %+v", visibility)
	}
	for _, audience := range visibility.Audiences {
		if len(visibility.Policy[audience]) != len(domain.AllCategories) {
			t.Fatalf("%s is not open by default: %v", audience, visibility.Policy[audience])
		}
	}
	ride := owner.upload("public-default.gpx", gpxRide(time.Date(2021, 8, 1, 9, 0, 0, 0, time.UTC)))
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
	owner.do(http.MethodPut, policyPath, map[string]any{"policy": map[string][]string{}}, http.StatusOK)
	viewer.do(http.MethodGet, laps, nil, http.StatusNotFound)
	owner.do(http.MethodGet, laps, nil, http.StatusOK)
	var saved struct {
		Policy map[string][]string `json:"policy"`
	}
	if err := json.Unmarshal(owner.do(http.MethodGet, policyPath, nil, http.StatusOK), &saved); err != nil || len(saved.Policy["public"]) != 0 {
		t.Fatalf("explicit empty policy = %+v, err = %v", saved, err)
	}
	owner.do(http.MethodPut, "/api/profile/public", map[string]bool{"public": false}, http.StatusOK)
	viewer.do(http.MethodGet, profilePath, nil, http.StatusForbidden)
}
