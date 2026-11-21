package garminsync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type connectionRow struct {
	state   domain.GarminSync
	profile string
	session domain.GarminSession
}

type syncRepo struct {
	rows map[domain.UserID]connectionRow
}

func (r *syncRepo) Connect(_ context.Context, user domain.UserID, profile string, session domain.GarminSession, now time.Time) error {
	row, exists := r.rows[user]
	if exists && profile != row.profile {
		return port.ErrGarminAccount
	}
	row.profile, row.session = profile, session
	row.state.UserID, row.state.NextRun = user, &now
	row.state.Lease, row.state.LeaseUntil = "", nil
	row.state.NeedsKey, row.state.Error, row.state.Failures = false, "", 0
	r.rows[user] = row
	return nil
}

func (r *syncRepo) Disconnect(_ context.Context, user domain.UserID) error {
	delete(r.rows, user)
	return nil
}

func (r *syncRepo) Status(_ context.Context, user domain.UserID) (domain.GarminSync, error) {
	row, ok := r.rows[user]
	if !ok {
		return row.state, domain.ErrNotFound
	}
	return row.state, nil
}

func (r *syncRepo) Queue(_ context.Context, user domain.UserID, now time.Time) error {
	row, ok := r.rows[user]
	if !ok || row.state.NeedsKey {
		return domain.ErrNotFound
	}
	if row.state.Error != "rate_limit" {
		row.state.NextRun = &now
		r.rows[user] = row
	}
	return nil
}

func (r *syncRepo) Claim(_ context.Context, now, expires time.Time) (domain.GarminSync, domain.GarminSession, error) {
	for user, row := range r.rows {
		s := &row.state
		if s.NeedsKey || s.NextRun == nil || s.NextRun.After(now) || s.LeaseUntil != nil && s.LeaseUntil.After(now) {
			continue
		}
		s.Lease, s.LeaseUntil = uuid.NewString(), &expires
		if s.Window == nil {
			s.Window = &now
		}
		r.rows[user] = row
		return *s, row.session, nil
	}
	return domain.GarminSync{}, domain.GarminSession{}, domain.ErrNotFound
}

func (r *syncRepo) Guard(_ context.Context, user domain.UserID, lease string) error {
	row, ok := r.rows[user]
	if !ok || row.state.Lease != lease {
		return domain.ErrNotFound
	}
	return nil
}

func (r *syncRepo) Checkpoint(ctx context.Context, state domain.GarminSync, session *domain.GarminSession, release bool) error {
	if err := r.Guard(ctx, state.UserID, state.Lease); err != nil {
		return err
	}
	row := r.rows[state.UserID]
	if session != nil {
		row.session = *session
	}
	if release {
		state.Lease, state.LeaseUntil = "", nil
	}
	row.state = state
	r.rows[state.UserID] = row
	return nil
}

type remote struct {
	now         *time.Time
	keys        map[string]string
	tokens      map[string]string
	rides       map[string][]domain.GarminActivity
	offsets     []int
	lastFrom    time.Time
	lastThrough time.Time
	fail        map[string]error
	downloads   map[string]int
	afterFetch  func()
	exchanges   int
}

func (r *remote) ExchangeKey(_ context.Context, key string) (domain.GarminSession, error) {
	r.exchanges++
	profile, ok := r.keys[key]
	if !ok {
		return domain.GarminSession{}, port.ErrGarminUnauthorized
	}
	token := fmt.Sprintf("%s-token-%d", profile, r.exchanges)
	r.tokens[token] = profile
	return domain.GarminSession{Key: key, Token: token, ExpiresAt: r.now.Add(24 * time.Hour)}, nil
}

func (r *remote) ProfileHandle(_ context.Context, token string) (string, error) {
	return r.tokens[token], nil
}

func (r *remote) Rides(_ context.Context, token string, from, through time.Time, offset, limit int) ([]domain.GarminActivity, error) {
	profile, ok := r.tokens[token]
	if !ok {
		return nil, port.ErrGarminUnauthorized
	}
	r.offsets = append(r.offsets, offset)
	r.lastFrom, r.lastThrough = from, through
	var list []domain.GarminActivity
	for _, ride := range r.rides[profile] {
		if !ride.LocalDay.IsZero() && (ride.LocalDay.After(through) || !from.IsZero() && ride.LocalDay.Format(time.DateOnly) < from.Format(time.DateOnly)) {
			continue
		}
		list = append(list, ride)
	}
	offset = min(offset, len(list))
	return list[offset:min(offset+limit, len(list))], nil
}

func (r *remote) Original(_ context.Context, token, id string) (port.UploadInput, error) {
	profile, ok := r.tokens[token]
	if !ok {
		return port.UploadInput{}, port.ErrGarminUnauthorized
	}
	key := profile + "-" + id
	r.downloads[key]++
	if r.afterFetch != nil {
		r.afterFetch()
	}
	if err := r.fail[key]; err != nil {
		return port.UploadInput{}, err
	}
	return port.UploadInput{Filename: id + ".fit", Raw: []byte("fixture")}, nil
}

type rideSink struct {
	repo    *syncRepo
	byOwner map[domain.UserID]map[string]bool
}

func (s *rideSink) Existing(_ context.Context, user domain.UserID, id string) (bool, bool, error) {
	hasStream, ok := s.byOwner[user][id]
	return ok, hasStream, nil
}

func (s *rideSink) IngestRide(ctx context.Context, job domain.GarminSync, ride domain.GarminActivity, file port.UploadInput) (bool, error) {
	if err := s.repo.Guard(ctx, job.UserID, job.Lease); err != nil {
		return false, err
	}
	if s.byOwner[job.UserID] == nil {
		s.byOwner[job.UserID] = make(map[string]bool)
	}
	hasStream := len(file.Raw) > 0
	s.byOwner[job.UserID][ride.ID] = hasStream
	return hasStream, nil
}

type scenario struct {
	*Connections
	t      *testing.T
	time   time.Time
	repo   *syncRepo
	remote *remote
	sink   *rideSink
}

func newScenario(t *testing.T) *scenario {
	s := &scenario{t: t, time: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)}
	s.repo = &syncRepo{rows: make(map[domain.UserID]connectionRow)}
	s.remote = &remote{
		now: &s.time, keys: map[string]string{"test-A": "a", "test-B": "b", "rotated-A": "a"},
		tokens: make(map[string]string), rides: make(map[string][]domain.GarminActivity),
		fail: make(map[string]error), downloads: make(map[string]int),
	}
	s.sink = &rideSink{repo: s.repo, byOwner: make(map[domain.UserID]map[string]bool)}
	s.Connections = New(s.repo, s.remote, s.sink, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Now = func() time.Time { return s.time }
	return s
}

func (s *scenario) connect(user domain.UserID, key string) {
	s.t.Helper()
	if err := s.Connect(s.t.Context(), user, key); err != nil {
		s.t.Fatal(err)
	}
}

func (s *scenario) run(want bool) {
	s.t.Helper()
	if ran, err := s.RunOne(s.t.Context()); err != nil || ran != want {
		s.t.Fatalf("ran = %v, want %v, error %v", ran, want, err)
	}
}

func TestBackfillResumesAndIncrementalSyncSkipsExisting(t *testing.T) {
	s := newScenario(t)
	user := domain.UserID(uuid.New())
	for i := 1; i <= pageSize+2; i++ {
		s.remote.rides["a"] = append(s.remote.rides["a"], domain.GarminActivity{ID: fmt.Sprint(i)})
	}
	s.remote.fail["a-4"] = &port.GarminRateLimit{RetryAfter: 4 * time.Minute}
	s.remote.fail["a-7"] = port.ErrGarminNoFile
	s.connect(user, "test-A")
	s.run(true)
	state := s.repo.rows[user].state
	if state.Offset != 3 || state.HistoryRides != 3 || state.Error != "rate_limit" || !state.NextRun.Equal(s.time.Add(4*time.Minute)) {
		t.Fatalf("partial page checkpoint: %+v", state)
	}
	if err := s.SyncNow(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	s.run(false) // Manual sync respects Garmin's retry window.
	delete(s.remote.fail, "a-4")
	s.time = s.time.Add(4 * time.Minute)

	// Simulate a server restart using the same persisted cursor and session.
	s.Connections = New(s.repo, s.remote, s.sink, time.Hour, s.Log)
	s.Now = func() time.Time { return s.time }
	s.run(true)
	s.time = s.time.Add(time.Second)
	s.run(true)
	if !slices.Equal(s.remote.offsets, []int{0, 3, 12}) {
		t.Fatalf("pagination did not resume: %v", s.remote.offsets)
	}
	state = s.repo.rows[user].state
	if !state.HistoryComplete || state.HistoryRides != 12 || state.SummaryOnly != 1 || s.sink.byOwner[user]["7"] || state.LastSync == nil {
		t.Fatalf("unexpected history status: %+v", state)
	}
	started := *state.Watermark
	s.remote.rides["a"] = append([]domain.GarminActivity{{ID: "13"}}, s.remote.rides["a"]...)
	s.run(false)
	s.time = s.time.Add(time.Hour)
	for range 3 {
		s.run(true)
		s.time = s.time.Add(time.Second)
	}
	if len(s.sink.byOwner[user]) != 13 || !s.sink.byOwner[user]["13"] ||
		s.remote.downloads["a-1"] != 1 || s.remote.downloads["a-4"] != 2 || s.remote.downloads["a-7"] != 1 ||
		!s.remote.lastFrom.Equal(started.AddDate(0, 0, -overlapDays)) || s.remote.exchanges != 1 {
		t.Fatalf("incremental sync = %v, downloads = %v", s.sink.byOwner[user], s.remote.downloads)
	}
}

func TestAccountIsolationReconnectionAndDisconnectDuringDownload(t *testing.T) {
	s := newScenario(t)
	a, b := domain.UserID(uuid.New()), domain.UserID(uuid.New())
	s.remote.rides["a"] = []domain.GarminActivity{{ID: "1"}, {ID: "2"}}
	s.remote.rides["b"] = []domain.GarminActivity{{ID: "1"}}
	s.connect(a, "test-A")
	if _, err := s.Status(t.Context(), b); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another user could read status: %v", err)
	}
	s.connect(b, "test-B")
	if err := s.Connect(t.Context(), a, "test-B"); err == nil {
		t.Fatal("a different profile replaced the current account")
	}
	s.run(true)
	s.run(true)
	if len(s.sink.byOwner[a]) != 2 || len(s.sink.byOwner[b]) != 1 || s.remote.downloads["a-1"] != 1 || s.remote.downloads["b-1"] != 1 {
		t.Fatalf("rides crossed account boundaries: %v", s.sink.byOwner)
	}
	token := s.repo.rows[a].session.Token
	delete(s.remote.tokens, token)
	delete(s.remote.keys, "test-A")
	s.time = s.time.Add(time.Second)
	s.run(true)
	s.run(true)
	if state := s.repo.rows[a].state; !state.NeedsKey || state.Offset != 2 || state.Error != "key_expired" {
		t.Fatalf("expired key lost the cursor: %+v", state)
	}
	s.connect(a, "rotated-A")
	s.run(true)
	if state := s.repo.rows[a].state; !state.HistoryComplete || state.HistoryRides != 2 {
		t.Fatalf("key replacement did not resume: %+v", state)
	}
	s.remote.rides["a"] = append([]domain.GarminActivity{{ID: "3"}}, s.remote.rides["a"]...)
	if err := s.SyncNow(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	s.remote.afterFetch = func() {
		if err := s.Disconnect(t.Context(), a); err != nil {
			t.Fatal(err)
		}
	}
	s.run(true)
	if len(s.sink.byOwner[a]) != 2 || len(s.sink.byOwner[b]) != 1 {
		t.Fatal("a stale import landed after disconnect")
	}
	if _, err := s.Status(t.Context(), a); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("disconnected state = %v", err)
	}
}

func TestHistoryCursorSurvivesChangesOnNewerDays(t *testing.T) {
	s := newScenario(t)
	user := domain.UserID(uuid.New())
	day := s.time.Truncate(24*time.Hour).AddDate(0, 0, -5)
	s.remote.rides["a"] = []domain.GarminActivity{
		{ID: "3", LocalDay: day.AddDate(0, 0, 1)},
		{ID: "2", LocalDay: day},
		{ID: "1", LocalDay: day},
	}
	s.remote.fail["a-1"] = errors.New("temporary download failure")
	s.connect(user, "test-A")
	s.run(true)
	state := s.repo.rows[user].state
	if state.CursorDay == nil || !state.CursorDay.Equal(day) || state.Offset != 1 || state.HistoryRides != 2 {
		t.Fatalf("cursor did not advance to the older day: %+v", state)
	}
	// Deleting a newer ride must not shift the offset on the current day.
	s.remote.rides["a"] = s.remote.rides["a"][1:]
	delete(s.remote.fail, "a-1")
	s.time = *state.NextRun
	s.run(true)
	if !s.remote.lastThrough.Equal(day) || !s.sink.byOwner[user]["1"] {
		t.Fatal("resume missed the next ride on the checkpoint date")
	}
}
