//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	cairnv1 "github.com/opencairn-org/cairn-core/gen/proto/cairn/v1"
	workerv1 "github.com/opencairn-org/cairn-core/gen/proto/cairn/worker/v1"
	"github.com/opencairn-org/cairn-core/internal/adapter/primary/rest"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

const stravaFixturePool = "strava-fetcher"
const stravaFixturePackage = "cairns.internal.strava-importer"

type stravaTestExchanger struct{}

func (stravaTestExchanger) ExchangeCode(_ context.Context, _, _ string, _ domain.UserProviderConfig, in port.CodeExchange) (port.TokenState, string, error) {
	if in.Code == "bad" {
		return port.TokenState{}, "", errors.New("fixture API unavailable")
	}
	return port.TokenState{
		AccessToken: "fixture-access-" + in.Code, RefreshToken: "fixture-refresh-" + in.Code,
		ExpiresAt: time.Now().Add(6 * time.Hour),
	}, in.Code, nil
}

func registerStravaFixture(t *testing.T, env *itestEnv) {
	t.Helper()
	env.app.CodeExchanger = stravaTestExchanger{}
	if err := env.app.Providers.Upsert(t.Context(), domain.ProviderMeta{
		ID: "strava", DisplayName: "Strava", AuthMethod: domain.AuthMethodOAuth2,
		OAuth2: &domain.ProviderOAuth{AuthorizeURL: "https://www.strava.com/oauth/authorize", Scopes: []string{"activity:read_all"}},
	}, stravaFixturePackage); err != nil {
		t.Fatal(err)
	}
	if err := env.app.WorkerPools.Upsert(t.Context(), domain.WorkerPool{
		Name: stravaFixturePool, Provider: "strava", Package: stravaFixturePackage,
	}); err != nil {
		t.Fatal(err)
	}
}

func stravaConnection(c *itestClient) string {
	return str(c.post("/api/connections", map[string]any{"provider": "strava", "client_id": "123", "client_secret": "fixture-secret"}, http.StatusCreated), "id")
}

func stravaCallback(t *testing.T, c *itestClient, connection, code, scope string) {
	t.Helper()
	c.do(http.MethodGet, "/auth/oauth/strava/connect?connection="+connection, nil, http.StatusFound)
	callback := mustURL(t, c.env.srv.URL+"/auth/oauth/strava/callback")
	for _, cookie := range c.http.Jar.Cookies(callback) {
		if cookie.Name == rest.OAuthConnectStateCookie {
			callback.RawQuery = url.Values{"state": {cookie.Value}, "code": {code}, "scope": {scope}}.Encode()
			c.do(http.MethodGet, callback.RequestURI(), nil, http.StatusFound)
			return
		}
	}
	t.Fatal("OAuth state cookie missing")
}

func stravaAccount(t *testing.T, c *itestClient, remoteID string) domain.ExternalAccount {
	t.Helper()
	list, err := c.env.app.ExternalAccounts.ListAccountsForUser(t.Context(), c.userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.ProviderAccountID == remoteID {
			return a
		}
	}
	t.Fatalf("Strava athlete %s not linked", remoteID)
	return domain.ExternalAccount{}
}

func TestAPI_StravaConnectionLifecycle(t *testing.T) {
	env := newITDiscoveryEnv(t)
	ctx := t.Context()
	registerStravaFixture(t, env)
	alice := env.user("strava-alice", domain.UserRoleUser)
	bob := env.user("strava-bob", domain.UserRoleUser)
	conn := stravaConnection(alice)
	stravaCallback(t, alice, conn, "bad", "activity:read_all")
	stravaCallback(t, alice, conn, "1001", "activity:read")
	if accounts, err := env.app.ExternalAccounts.ListAccountsForUser(ctx, alice.userID); err != nil || len(accounts) != 0 {
		t.Fatalf("failure or insufficient consent linked an account: %v", err)
	}
	stravaCallback(t, alice, conn, "1001", "read,activity:read_all")
	a := stravaAccount(t, alice, "1001")
	stravaCallback(t, bob, stravaConnection(bob), "1002", "activity:read_all")
	b := stravaAccount(t, bob, "1002")
	if a.ID == b.ID || a.UserID == b.UserID {
		t.Fatal("the two athletes shared an account")
	}
	bob.do(http.MethodPost, "/api/accounts/"+a.ID.String()+"/sync/now", nil, http.StatusNotFound)
	alice.do(http.MethodPost, "/api/accounts/"+a.ID.String()+"/sync/now", nil, http.StatusAccepted)

	first, err := env.app.OAuthTokens.GetToken(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	rotated := first
	rotated.AccessToken, rotated.RefreshToken = "rotated-access", "rotated-refresh"
	rotated.ExpiresAt = first.ExpiresAt.Add(time.Hour)
	if err := env.app.OAuthTokens.StoreToken(ctx, a.ID, port.StoreTokenInput{State: rotated, PreviousExpiresAt: first.ExpiresAt}); err != nil {
		t.Fatal(err)
	}
	if err := env.app.OAuthTokens.MarkNeedsReauth(ctx, a.ID, "invalid_grant"); err != nil {
		t.Fatal(err)
	}
	if got := stravaAccount(t, alice, "1001"); got.Status != domain.ExternalAccountStatusAuthInvalid {
		t.Fatalf("auth failure status = %s", got.Status)
	}
	alice.do(http.MethodPost, "/api/accounts/"+a.ID.String()+"/sync/now", nil, http.StatusConflict)
	stravaCallback(t, alice, conn, "1001", "activity:read_all")
	if got, err := env.app.OAuthTokens.GetToken(ctx, a.ID); err != nil || got.AccessToken != first.AccessToken {
		t.Fatalf("reauthorization did not replace the old token: %v", err)
	}

	start := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	ride := importStravaFixture(t, env, a, "501", start, 1, cairnv1.ActivityType_ACTIVITY_TYPE_RIDE)
	alice.do(http.MethodDelete, "/api/connections/"+conn, nil, http.StatusOK)
	if _, err := env.app.OAuthTokens.GetToken(ctx, a.ID); !errors.Is(err, port.ErrTokenAccountNotFound) {
		t.Fatalf("the disconnected token was still usable: %v", err)
	}
	if got := stravaSource(t, env, a, "501"); got.ActivityID != ride.ActivityID {
		t.Fatal("disconnect changed the imported ride")
	}
	stravaCallback(t, alice, stravaConnection(alice), "1001", "activity:read_all")
	if got := stravaAccount(t, alice, "1001"); got.ID != a.ID || got.Status != domain.ExternalAccountStatusActive {
		t.Fatalf("reconnect did not reuse source identity: %s %s", got.ID, got.Status)
	}
	if got, err := env.app.OAuthTokens.GetToken(ctx, b.ID); err != nil || got.AccessToken != "fixture-access-1002" {
		t.Fatalf("Alice's changes affected Bob: %v", err)
	}
}

func TestAPI_StravaCrossProviderMerge(t *testing.T) {
	env := newITestEnv(t)
	env.app.Pipeline.Bus = nil
	registerStravaFixture(t, env)
	ctx := t.Context()
	viewer := env.user("strava-viewer", domain.UserRoleUser)

	for i, order := range [][]string{
		{"garmin", "strava"}, {"strava", "garmin"}, {"upload", "garmin", "strava"},
	} {
		rider := env.user("strava-rider-"+order[0], domain.UserRoleUser)
		stravaCallback(t, rider, stravaConnection(rider), "athlete-"+order[0], "activity:read_all")
		a := stravaAccount(t, rider, "athlete-"+order[0])
		start := time.Now().UTC().AddDate(0, 0, -5-i).Truncate(time.Second)
		fit := garminTestFIT(t, start)
		catalog := createStravaMergeSegment(t, env, i+1)
		var independent domain.ActivityID
		for _, provider := range order {
			switch provider {
			case "upload":
				up := rider.upload("manual.fit", fit)
				independent, _ = domain.ParseUUID[domain.ActivityID](str(up, "activity_id"))
			case "garmin":
				independent = importGarminFITFixture(t, env, rider, start, fit)
			case "strava":
				source := importStravaFixture(t, env, a, "same", start.Add(25*time.Second), 1, cairnv1.ActivityType_ACTIVITY_TYPE_RIDE)
				if independent == (domain.ActivityID{}) {
					act, err := env.app.Activities.GetActivity(ctx, source.ActivityID)
					if err != nil || act.MovingDuration != source.Parsed.MovingDuration ||
						act.Summary.AvgSpeedMps == nil || *act.Summary.AvgSpeedMps != *act.Summary.DistanceM/act.MovingDuration.Seconds() {
						t.Fatalf("Strava-only timing not preserved: %+v, %v", act, err)
					}
					// Access checks before an independent ride exists.
					viewer.rpc("/cairn.v1.ActivityService/GetActivity", map[string]any{"id": source.ActivityID.String()}, http.StatusNotFound)
					hidden, err := env.app.Stats.Get(port.WithActivityReadViewer(ctx, false), rider.userID)
					if err != nil || hidden.Totals.Count != 0 {
						t.Fatalf("Strava-only ride leaked into community stats: %+v, %v", hidden.Totals, err)
					}
				}
			}
		}

		source := stravaSource(t, env, a, "same")
		repeated := importStravaFixture(t, env, a, "same", start.Add(25*time.Second), 1, cairnv1.ActivityType_ACTIVITY_TYPE_RIDE)
		if source.ID != repeated.ID || independent != repeated.ActivityID {
			t.Fatal("a repeat import did not preserve its source and canonical activity")
		}
		act, err := env.app.Activities.GetActivity(ctx, independent)
		if err != nil || act.ProviderRestricted || act.Title == "Strava-only title" ||
			act.Summary.AvgPowerW == nil || *act.Summary.AvgPowerW != 225 {
			t.Fatalf("canonical data did not come from the independent recording: %+v, %v", act, err)
		}
		sources, err := env.app.Activities.ListSourcesForActivity(ctx, independent)
		if err != nil || len(sources) != len(order) {
			t.Fatalf("source attribution count = %d, want %d: %v", len(sources), len(order), err)
		}
		if activity := viewer.do(http.MethodPost, "/cairn.v1.ActivityService/GetActivity", map[string]any{"id": independent.String()}, http.StatusOK); bytes.Contains(activity, []byte(source.ID.String())) {
			t.Fatal("community detail disclosed the Strava source")
		}
		viewer.rpc("/cairn.v1.ActivityService/GetActivityStream", map[string]any{"activitySourceId": source.ID.String()}, http.StatusNotFound)
		stats, err := env.app.Stats.Get(ctx, rider.userID)
		if err != nil || stats.Totals.Count != 1 || stats.Totals.MovingS != 400 || math.Abs(stats.Totals.DistanceM-2540) > 5 {
			t.Fatalf("stats counted more than one ride: %+v, %v", stats.Totals, err)
		}
		if act.Summary.ElevationGainM == nil || stats.Totals.ElevationGainM != *act.Summary.ElevationGainM {
			t.Fatalf("elevation was not counted once: %+v", stats.Totals)
		}
		load, err := env.app.Stats.TrainingLoad(ctx, rider.userID, 30)
		var tss float64
		for _, day := range load {
			tss += day.TSS
		}
		if err != nil || act.Summary.TSS == nil || math.Abs(tss-*act.Summary.TSS) > .1 {
			t.Fatalf("training stress was not counted once: total %v, error %v", tss, err)
		}
		heatmap, err := env.app.Stats.Heatmap(ctx, rider.userID, port.ActivityListFilter{}, domain.StreamResolutionRaw)
		if err != nil || heatmap.Matched != 1 || len(heatmap.Tracks) != 1 {
			t.Fatalf("heatmap counted more than one ride: %+v, %v", heatmap, err)
		}
		records, err := env.app.Stats.PersonalRecords(ctx, rider.userID)
		if err != nil || len(records) == 0 {
			t.Fatalf("records did not survive merge: %+v, %v", records, err)
		}
		for _, entry := range records {
			if entry.ActivityID != independent || (entry.Metric == domain.BestEffortMetricPower && entry.AchievedValue != 225) {
				t.Fatalf("record on a stale or inferior source: %+v", entry)
			}
		}
		efforts := activitySegmentEfforts(t, rider, independent.String())
		count := 0
		for _, effort := range efforts {
			if effort.SegmentID == catalog.String() {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("segment has %d efforts, want 1", count)
		}

		// A close timestamp and equal distance still describe a different ride
		// when progress through the course diverges.
		other := importStravaFixture(t, env, a, "other", start.Add(15*time.Second), -1, cairnv1.ActivityType_ACTIVITY_TYPE_RIDE)
		if other.ActivityID == independent {
			t.Fatal("different route matched the existing ride")
		}
		importStravaFixture(t, env, a, "running", start, 1, cairnv1.ActivityType_ACTIVITY_TYPE_RUN)
		if _, err := env.app.Activities.FindOwnedSourceByExternalID(ctx, rider.userID, "strava", &a.ID, "running"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Strava non-cycling activity was accepted: %v", err)
		}
	}
}

func importGarminFITFixture(t *testing.T, env *itestEnv, rider *itestClient, start time.Time, fit []byte) domain.ActivityID {
	t.Helper()
	ride := domain.GarminActivity{ID: "garmin-fit", Title: "Garmin FIT ride", Type: "road_cycling", StartTime: start, Elapsed: 400 * time.Second, Moving: 400 * time.Second}
	payload, stream, err := garminPayload(ride, port.UploadInput{Filename: "garmin.fit", Raw: fit})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ingestActivityEvent(t.Context(), env.app, testLogger(),
		&workerv1.JobResult{WorkerName: "garmin-connect", WorkerVersion: "1.0"},
		&workerv1.ImportedActivity{
			Ref:     &workerv1.ExternalRef{UserId: rider.userID.String(), Provider: "garmin", ExternalId: ride.ID},
			Payload: payload, Stream: stream,
		})
	if err != nil {
		t.Fatal(err)
	}
	return out.ActivityID
}

func importStravaFixture(t *testing.T, env *itestEnv, a domain.ExternalAccount, id string, start time.Time, direction float64, kind cairnv1.ActivityType) domain.ActivitySource {
	t.Helper()
	stream := &cairnv1.ActivityStream{
		Channels: []cairnv1.StreamChannel{
			cairnv1.StreamChannel_STREAM_CHANNEL_LATITUDE, cairnv1.StreamChannel_STREAM_CHANNEL_LONGITUDE,
			cairnv1.StreamChannel_STREAM_CHANNEL_DISTANCE, cairnv1.StreamChannel_STREAM_CHANNEL_POWER,
		},
	}
	for i := 0; i <= 400; i += 4 {
		stream.TimeS = append(stream.TimeS, float64(i))
		stream.Latitude = append(stream.Latitude, 47+direction*float64(i)/20_000)
		stream.Longitude = append(stream.Longitude, 8+float64(i)*0.00004)
		stream.DistanceM = append(stream.DistanceM, float64(i)*6.25)
		stream.PowerW = append(stream.PowerW, 333)
	}
	stream.SampleCount = int32(len(stream.TimeS))
	duration := durationpb.New(400 * time.Second)
	ia := &workerv1.ImportedActivity{
		Ref: &workerv1.ExternalRef{UserId: a.UserID.String(), Provider: "strava", ExternalAccountId: a.ID.String(), ExternalId: id},
		Payload: &cairnv1.ActivitySourcePayload{
			Type: kind, Timezone: "UTC", StartTime: timestamppb.New(start),
			ElapsedDuration: duration, MovingDuration: durationpb.New(350 * time.Second),
			Title: "Strava-only title", Summary: &cairnv1.ActivitySummary{DistanceM: new(2500.0), AvgPowerW: new(int32(333))},
		},
		Stream: stream,
	}
	jr := &workerv1.JobResult{WorkerName: stravaFixturePool, WorkerPackage: stravaFixturePackage, WorkerVersion: "1.1", WorkerRelease: "v0.7.2"}
	if err := processActivityEvent(t.Context(), env.app, testLogger(), jr, &ingestBundle{provider: "strava", job: "fetch_source"}, ia); err != nil {
		t.Fatal(err)
	}
	if kind != cairnv1.ActivityType_ACTIVITY_TYPE_RIDE {
		return domain.ActivitySource{}
	}
	return stravaSource(t, env, a, id)
}

func stravaSource(t *testing.T, env *itestEnv, a domain.ExternalAccount, id string) domain.ActivitySource {
	t.Helper()
	src, err := env.app.Activities.FindOwnedSourceByExternalID(t.Context(), a.UserID, "strava", &a.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func createStravaMergeSegment(t *testing.T, env *itestEnv, n int) domain.SegmentID {
	t.Helper()
	external, provider := fmt.Sprint(n), "strava-test-catalog"
	def := domain.Segment{
		ID: domain.SegmentID(uuid.New()), Source: domain.SegmentSourceStravaCatalog,
		ExternalID: &external, CatalogProvider: &provider, Scope: domain.SegmentScopeInstance,
		Name: "Merge test segment", ActivityType: domain.ActivityTypeRide,
		Polyline: "gt{}G_hzo@o}@_q@", PolylinePrecision: 5,
	}
	points, err := def.DecodedPolyline()
	if err != nil {
		t.Fatal(err)
	}
	def.DistanceM = domain.PolylineDistanceM(points)
	if err := env.app.Segments.SaveSegment(t.Context(), def); err != nil {
		t.Fatal(err)
	}
	return def.ID
}

func TestAPI_StravaHistoricalDiscoveryPagination(t *testing.T) {
	env := newITDiscoveryEnv(t)
	registerStravaFixture(t, env)
	rider := env.user("strava-history", domain.UserRoleUser)
	stravaCallback(t, rider, stravaConnection(rider), "athlete-history", "activity:read_all")
	a := stravaAccount(t, rider, "athlete-history")
	responder, err := env.app.NATSBus.RespondTo(t.Context(), domain.DiscoverSubject("strava", stravaFixturePool),
		func(_ context.Context, body []byte) ([]byte, error) {
			var req struct {
				AccountID string `json:"account_id"`
				StartPage int    `json:"start_page"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, err
			}
			if req.AccountID != a.ID.String() {
				return nil, errors.New("wrong account")
			}
			if req.StartPage <= 1 {
				return []byte(`{"items":[{"item_type":"activity","external_id":"100"}],"next_page":2}`), nil
			}
			return []byte(`{"items":[{"item_type":"activity","external_id":"101"}],"complete":true}`), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = responder.Close(context.Background()) })
	imports := syncImports(env.app, testLogger())
	for range 2 {
		imports.RunFullDiscovery(a, true)
	}
	counts, err := env.app.ImportQueue.CountByStatus(t.Context(), a.ID)
	if err != nil || counts[domain.ImportStatusPending] != 2 {
		t.Fatalf("repeated history sync did not keep exactly one queue item per ride: %v, %v", counts, err)
	}
}
