package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/inmem"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
	"github.com/opencairn-org/cairn-core/internal/usecase/pipeline"
	"github.com/opencairn-org/cairn-core/internal/usecase/trainingload"
)

type trainingLoadCounter struct {
	mu    sync.Mutex
	users []domain.UserID
}

func (f *trainingLoadCounter) Execute(_ context.Context, in trainingload.Input) (trainingload.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users = append(f.users, in.UserID)
	return trainingload.Result{}, nil
}

func (f *trainingLoadCounter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.users)
}

// followUpPipeline holds one activity whose only source is detached, so a remerge soft-deletes it.
func followUpPipeline(t *testing.T) (*pipeline.Pipeline, *trainingLoadCounter, domain.ActivityID) {
	t.Helper()
	repo := inmem.NewActivityRepo()
	now := time.Now().UTC()
	uid, aid, sid := domain.UserID(uuid.New()), domain.ActivityID(uuid.New()), domain.SourceID(uuid.New())
	repo.Activities[aid] = domain.Activity{ID: aid, UserID: uid, Type: domain.ActivityTypeRide, StartTime: now, Privacy: domain.PrivacyPrivate}
	repo.Sources[sid] = domain.ActivitySource{ID: sid, ActivityID: aid, UserID: uid, Provider: "garmin", ExternalID: "x", Status: domain.SourceStatusDetached, ImportedAt: now}
	load := &trainingLoadCounter{}
	recompute := activity.NewRecomputeActivityFromSources(repo, inmem.DefaultSettings{}, nil, nil, nil, nil, inmem.PassTx{}, time.Now)
	return &pipeline.Pipeline{Activities: repo, Recompute: recompute, TrainingLoad: load}, load, aid
}

func TestFollowUpEventRoundTrip(t *testing.T) {
	bus, _ := dlqTestBus(t)
	ctx := t.Context()
	if err := bus.BootstrapStreams(ctx); err != nil {
		t.Fatal(err)
	}
	p, load, aid := followUpPipeline(t)
	p.Bus = bus
	if _, err := p.Remerge(ctx, aid); err != nil {
		t.Fatal(err)
	}
	// Published, not run inline; the consumer created afterwards still picks it up.
	if load.count() != 0 {
		t.Fatal("follow-ups ran inline")
	}
	app := &App{NATSBus: bus, Pipeline: p}
	sub, err := startFollowUpConsumer(ctx, app, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Close(context.Background()) })
	deadline := time.Now().Add(10 * time.Second)
	for load.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if load.count() != 1 {
		t.Fatalf("follow-up did not run via the bus: %d recomputations", load.count())
	}
}

func TestFollowUpHandler(t *testing.T) {
	p, load, aid := followUpPipeline(t)
	h := followUpHandler(p, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var term *port.TerminalError
	if err := h(t.Context(), port.Message{Body: []byte(`{"kind":"nope"}`)}); !errors.As(err, &term) {
		t.Fatalf("bad payload: %v", err)
	}
	body, err := pipeline.ActivityChanged{Kind: pipeline.ChangeRemerged, UserID: domain.UserID(uuid.New()), ActivityID: aid, SoftDeleted: true}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	// Training load can be recomputed safely on redelivery.
	if err := h(t.Context(), port.Message{Body: body, DeliveryAttempt: 2}); err != nil || load.count() != 1 {
		t.Fatalf("redelivery: %v recomputations=%d", err, load.count())
	}
	if err := h(t.Context(), port.Message{Body: body, DeliveryAttempt: 1}); err != nil || load.count() != 2 {
		t.Fatalf("next delivery: %v recomputations=%d", err, load.count())
	}
}
