// Package garminsync connects a user's Garmin key and processes resumable ride imports.
package garminsync

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

const (
	pageSize      = 10
	batchTimeout  = 75 * time.Second
	leaseDuration = 2 * time.Minute
	overlapDays   = 14
)

type Connections struct {
	Repo     port.GarminSyncRepo
	API      port.GarminConnect
	Ingester port.GarminRideIngester
	// A zero cadence leaves recurring polls off. Queued imports can still run.
	PollInterval time.Duration
	Now          func() time.Time
	Log          *slog.Logger
	wake         chan struct{}
}

func New(repo port.GarminSyncRepo, api port.GarminConnect, ingester port.GarminRideIngester, poll time.Duration, log *slog.Logger) *Connections {
	return &Connections{Repo: repo, API: api, Ingester: ingester, PollInterval: poll, Log: log, wake: make(chan struct{}, 1)}
}

func (c *Connections) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Connections) Connect(ctx context.Context, user domain.UserID, key string) error {
	key = strings.TrimSpace(key)
	if len(key) == 0 || len(key) > 2048 || strings.ContainsFunc(key, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return port.NewActionError(port.ActionErrorInvalid, "invalid_key")
	}
	session, err := c.API.ExchangeKey(ctx, key)
	if err != nil {
		return connectionError(err)
	}
	profile, err := c.API.ProfileHandle(ctx, session.Token)
	if err != nil {
		return connectionError(err)
	}
	if err := c.Repo.Connect(ctx, user, profile, session, c.now()); err != nil {
		if errors.Is(err, port.ErrGarminAccount) {
			return port.NewActionError(port.ActionErrorConflict, "different_account")
		}
		return err
	}
	c.notify()
	return nil
}

func connectionError(err error) error {
	if errors.Is(err, port.ErrGarminUnauthorized) {
		return port.NewActionError(port.ActionErrorInvalid, "invalid_key")
	}
	var rate *port.GarminRateLimit
	if errors.As(err, &rate) {
		return port.NewActionError(port.ActionErrorUpstream, "rate_limit")
	}
	return port.NewActionError(port.ActionErrorUpstream, "garmin_unavailable")
}

func (c *Connections) Disconnect(ctx context.Context, user domain.UserID) error {
	return c.Repo.Disconnect(ctx, user)
}

func (c *Connections) Status(ctx context.Context, user domain.UserID) (domain.GarminSync, error) {
	return c.Repo.Status(ctx, user)
}

func (c *Connections) SyncNow(ctx context.Context, user domain.UserID) error {
	if err := c.Repo.Queue(ctx, user, c.now()); err != nil {
		return err
	}
	c.notify()
	return nil
}

func (c *Connections) notify() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Run shares the instance scheduler configuration and wake-up lifecycle. It
// drains one page per claimed connection and only runs one batch at a time.
func (c *Connections) Run(ctx context.Context, interval time.Duration, accountsPerTick int) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(min(interval, 10*time.Second))
	defer ticker.Stop()
	for {
		for range max(1, accountsPerTick) {
			claimed, err := c.RunOne(ctx)
			if err != nil && ctx.Err() == nil {
				c.Log.Warn("Garmin Connect scheduler failed", "error", err)
			}
			if err != nil || !claimed {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-c.wake:
		}
	}
}

// RunOne claims and processes at most one page of history or incremental sync.
func (c *Connections) RunOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	now := c.now()
	state, session, err := c.Repo.Claim(ctx, now, now.Add(leaseDuration))
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	updatedSession := session
	batchCtx, cancel := context.WithTimeout(ctx, batchTimeout)
	err = c.importPage(batchCtx, &state, &updatedSession)
	cancel()
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if errors.Is(err, domain.ErrNotFound) {
		// The user disconnected or refreshed the key while the batch was running.
		return true, nil
	}
	if err != nil {
		c.retry(&state, err)
		c.Log.Warn("Garmin Connect batch could not finish", "user_id", state.UserID, "error", err)
	}
	var change *domain.GarminSession
	if updatedSession != session {
		change = &updatedSession
	}
	err = c.Repo.Checkpoint(ctx, state, change, true)
	if errors.Is(err, domain.ErrNotFound) {
		err = nil
	}
	return true, err
}

func (c *Connections) importPage(ctx context.Context, state *domain.GarminSync, session *domain.GarminSession) error {
	if session.Key == "" {
		return port.ErrGarminUnauthorized
	}
	if !session.ExpiresAt.After(c.now().Add(time.Minute)) {
		if err := c.renew(ctx, session); err != nil {
			return err
		}
	}
	var from time.Time
	if state.Watermark != nil {
		from = state.Watermark.AddDate(0, 0, -overlapDays)
	}
	// The search dates refer to the activity's local calendar. Include the
	// following date to cover devices ahead of UTC.
	through := state.Window.AddDate(0, 0, 1)
	if state.CursorDay != nil {
		through = *state.CursorDay
	}
	page, err := authorized(ctx, c, session, func(token string) ([]domain.GarminActivity, error) {
		return c.API.Rides(ctx, token, from, through, state.Offset, pageSize)
	})
	if err != nil {
		return err
	}
	for _, ride := range page {
		if err := c.importRide(ctx, state, session, ride); err != nil {
			return err
		}
		// Garmin sorts by local start time. Keep the offset within the oldest
		// day reached so edits or new rides on later days cannot shift it.
		if !ride.LocalDay.IsZero() && ride.LocalDay.Before(through) {
			through = ride.LocalDay
			state.CursorDay = new(ride.LocalDay)
			state.Offset = 0
		}
		state.Offset++
		// A restart can resume from the last recorded day and offset.
		if err := c.Repo.Checkpoint(ctx, *state, nil, false); err != nil {
			return err
		}
	}

	state.Failures, state.Error = 0, ""
	now := c.now()
	if len(page) != 0 {
		// Let other accounts claim a slot before requesting another page.
		state.NextRun = new(now.Add(time.Second))
		return nil
	}
	state.LastSync, state.Watermark = &now, state.Window
	state.HistoryComplete, state.Offset, state.Window, state.CursorDay = true, 0, nil, nil
	state.NextRun = nil
	if c.PollInterval > 0 {
		state.NextRun = new(now.Add(c.PollInterval))
	}
	return nil
}

func (c *Connections) importRide(ctx context.Context, state *domain.GarminSync, session *domain.GarminSession, ride domain.GarminActivity) error {
	if ride.ID == "" {
		state.Skipped++
		return nil
	}
	exists, stream, err := c.Ingester.Existing(ctx, state.UserID, ride.ID)
	if errors.Is(err, port.ErrGarminIgnored) {
		return nil
	}
	if err != nil {
		return err
	}
	if !exists {
		file, err := authorized(ctx, c, session, func(token string) (port.UploadInput, error) {
			return c.API.Original(ctx, token, ride.ID)
		})
		if err != nil && !errors.Is(err, port.ErrGarminNoFile) {
			return err
		}
		stream, err = c.Ingester.IngestRide(ctx, *state, ride, file)
		if errors.Is(err, port.ErrGarminIgnored) {
			return nil
		}
		if errors.Is(err, port.ErrGarminInvalidRide) {
			state.Skipped++
			return nil
		}
		if err != nil {
			return err
		}
	}
	if !state.HistoryComplete {
		state.HistoryRides++
	}
	if !stream && (!exists || !state.HistoryComplete) {
		state.SummaryOnly++
	}
	return nil
}

func (c *Connections) renew(ctx context.Context, session *domain.GarminSession) error {
	fresh, err := c.API.ExchangeKey(ctx, session.Key)
	if err == nil {
		*session = fresh
	}
	return err
}

// A bearer can expire or be revoked before the local renewal timer. Check the
// key with Garmin before asking the user to replace it.
func authorized[T any](ctx context.Context, c *Connections, session *domain.GarminSession, call func(string) (T, error)) (T, error) {
	result, err := call(session.Token)
	if !errors.Is(err, port.ErrGarminUnauthorized) {
		return result, err
	}
	if err := c.renew(ctx, session); err != nil {
		return result, err
	}
	return call(session.Token)
}

func (c *Connections) retry(state *domain.GarminSync, err error) {
	state.Failures++
	state.Error = "sync_failed"
	delay := time.Minute << min(state.Failures-1, 6)
	var rate *port.GarminRateLimit
	switch {
	case errors.Is(err, port.ErrGarminUnauthorized):
		state.Error, state.NeedsKey, state.NextRun = "key_expired", true, nil
		return
	case errors.As(err, &rate):
		state.Error = "rate_limit"
		delay = max(delay, rate.RetryAfter)
	default:
		var quota *port.ActionError
		if errors.As(err, &quota) && quota.Kind == port.ActionErrorQuota {
			state.Error = "quota"
		}
	}
	state.NextRun = new(c.now().Add(delay))
}
