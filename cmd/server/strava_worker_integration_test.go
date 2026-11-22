//go:build integration

package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// TestAPI_StravaWorkerRoundTrip runs the actual provider and SDK against
// Cairn's mock Strava API, real NATS and Postgres, and loopback blob storage.
func TestAPI_StravaWorkerRoundTrip(t *testing.T) {
	workerBin, mockBin := os.Getenv("PELOTON_TEST_STRAVA_WORKER"), os.Getenv("PELOTON_TEST_STRAVA_MOCK")
	if workerBin == "" || mockBin == "" {
		t.Skip("set PELOTON_TEST_STRAVA_WORKER and PELOTON_TEST_STRAVA_MOCK to the test binaries")
	}
	env := newITDiscoveryEnv(t)
	ctx := t.Context()
	env.app.BlobStore = newStravaTestBlobStore(t)
	var cleanup serveCleanup
	t.Cleanup(cleanup.run)
	if err := serveStartBusHandlers(ctx, env.app, testLogger(), &cleanup); err != nil {
		t.Fatal(err)
	}
	if err := serveStartPipelines(ctx, env.app, testLogger(), &cleanup); err != nil {
		t.Fatal(err)
	}
	go runImportQueueProcessor(ctx, testLogger(), env.app)

	addrFile := filepath.Join(t.TempDir(), "mock-url")
	startStravaProcess(t, mockBin, nil, "--listen", "127.0.0.1:0", "--addr-file", addrFile,
		"--activities", "2", "--seed-age", "48h", "--exchange-token-ttl", "1s")
	var mockURL string
	waitStrava(t, "mock API address", func() bool {
		raw, err := os.ReadFile(addrFile)
		mockURL = strings.TrimSpace(string(raw))
		return err == nil && mockURL != ""
	})
	startStravaProcess(t, workerBin, []string{
		"CAIRN_NATS_URL=" + os.Getenv("CAIRN_NATS_URL"),
		"CAIRN_WORKER_NAME=" + stravaFixturePool,
		"CAIRN_WORKER_ENROLLMENT_TOKEN=",
		"CAIRN_STRAVA_API_BASE_URL=" + mockURL + "/api/v3",
		"CAIRN_STRAVA_OAUTH_BASE_URL=" + mockURL + "/oauth",
		"CAIRN_STRAVA_CYCLING_ONLY=true",
	})
	waitStrava(t, "worker registration", func() bool {
		meta, err := env.app.Providers.Get(ctx, "strava")
		return err == nil && meta.OAuth2 != nil && strings.HasPrefix(meta.OAuth2.AuthorizeURL, mockURL)
	})

	rider := env.user("strava-worker-rider", domain.UserRoleUser)
	conn := stravaConnection(rider)
	connect, err := rider.http.Get(env.srv.URL + "/auth/oauth/strava/connect?connection=" + conn)
	if err != nil {
		t.Fatal(err)
	}
	_ = connect.Body.Close()
	authorize, err := rider.http.Get(connect.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	_ = authorize.Body.Close()
	callback := mustURL(t, authorize.Header.Get("Location"))
	rider.do(http.MethodGet, callback.RequestURI(), nil, http.StatusFound)
	a := stravaAccount(t, rider, "4242")
	rider.post("/api/accounts/"+a.ID.String()+"/sync/preview", nil, http.StatusOK)
	for range 2 {
		rider.post("/api/accounts/"+a.ID.String()+"/sync/start", map[string]bool{"skip_existing": true}, http.StatusOK)
		waitStrava(t, "two historical rides", func() bool {
			counts, err := env.app.ImportQueue.CountByStatus(ctx, a.ID)
			return err == nil && counts[domain.ImportStatusDone] == 2
		})
	}
	token, err := env.app.OAuthTokens.GetToken(ctx, a.ID)
	if err != nil || time.Until(token.ExpiresAt) < 5*time.Hour {
		t.Fatalf("worker did not refresh the short-lived token: %v", err)
	}

	resp, err := http.Post(mockURL+"/_mock/activities", "application/json", strings.NewReader(`{"name":"Incremental test ride"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mock create activity status %d", resp.StatusCode)
	}
	rider.do(http.MethodPost, "/api/accounts/"+a.ID.String()+"/sync/now", nil, http.StatusAccepted)
	waitStrava(t, "new ride via incremental sync", func() bool {
		totals, err := env.app.Stats.Get(ctx, rider.userID)
		return err == nil && totals.Totals.Count == 3
	})
	sources, err := env.app.Activities.ListSourcesForAccount(ctx, a.ID)
	if err != nil || len(sources) != 3 {
		t.Fatalf("worker created unexpected sources: %d, %v", len(sources), err)
	}
	for _, src := range sources {
		if src.Provider != "strava" || src.RawBlobID == "" || !src.Parsed.HasStream {
			t.Fatal("actual worker did not archive its raw recording or deliver the stream")
		}
	}
}

func startStravaProcess(t *testing.T, path string, env []string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), path, args...)
	cmd.Env = append(os.Environ(), env...)
	logPath := filepath.Join(t.TempDir(), "process.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = log.Close()
		if t.Failed() {
			raw, _ := os.ReadFile(logPath)
			t.Log(string(raw))
		}
	})
}

func waitStrava(t *testing.T, description string, ready func() bool) {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		if ready() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

type stravaTestBlobStore struct {
	port.BlobStore
	url  string
	mu   sync.Mutex
	data map[string][]byte
}

func newStravaTestBlobStore(t *testing.T) *stravaTestBlobStore {
	t.Helper()
	store := &stravaTestBlobStore{data: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		store.mu.Lock()
		store.data[r.URL.Query().Get("key")] = raw
		store.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	store.url = srv.URL
	return store
}

func (s *stravaTestBlobStore) PresignUpload(_ context.Context, key string, _ port.PresignUploadOpts) (port.PresignedURL, error) {
	return port.PresignedURL{URL: s.url + "/?key=" + url.QueryEscape(key)}, nil
}

func (s *stravaTestBlobStore) Get(_ context.Context, key string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.data[key]
	if !ok {
		return nil, "", domain.ErrNotFound
	}
	return bytes.Clone(raw), "application/json", nil
}

func (s *stravaTestBlobStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}
