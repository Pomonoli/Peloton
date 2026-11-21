package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	natsadapter "github.com/opencairn-org/cairn-core/internal/adapter/secondary/nats"
	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/tailwindkompass"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/pipeline"
	"github.com/opencairn-org/cairn-core/internal/usecase/segment"
)

func wireSegmentDiscovery(app *App, enabled bool, logger *slog.Logger) {
	if !enabled {
		return
	}
	if app.NATSBus == nil {
		logger.Warn("Strava segment discovery requires a NATS job bus")
		return
	}
	app.DiscoverSegments = &segment.DiscoverSegments{
		Provider: tailwindkompass.New(), Cache: app.Segments,
		Activities: app.Activities, Streams: app.Streams, Tx: app.Tx, Matcher: app.MatchSegments,
	}
}

func startSegmentDiscoveryConsumer(ctx context.Context, app *App, logger *slog.Logger) (port.Subscription, error) {
	if app.DiscoverSegments == nil {
		return nil, nil
	}
	logger.Info("starting Strava segment discovery consumer", "provider", app.DiscoverSegments.Provider.Name())
	return app.NATSBus.Subscribe(ctx, port.ConsumerConfig{
		Stream: natsadapter.StreamEvents, Durable: "strava-segment-discovery",
		// A newly enabled consumer starts with new events. Riders choose
		// which pre-existing activities to rescan.
		Subject: pipeline.ActivityChangedSubject, DeliverPolicy: port.DeliverNew,
		AckWait: 2 * time.Minute, MaxDeliver: 5,
	}, func(ctx context.Context, msg port.Message) error {
		ev, err := pipeline.DecodeActivityChanged(msg.Body)
		if err != nil {
			return &port.TerminalError{Reason: "bad_payload", Cause: err}
		}
		ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
		defer cancel()
		err = runSegmentDiscovery(ctx, app.DiscoverSegments, ev, logger)
		if err != nil {
			logger.Warn("segment discovery job failed", "activity_id", ev.ActivityID, "attempt", msg.DeliveryAttempt, "error", err)
		}
		return err
	})
}

func runSegmentDiscovery(ctx context.Context, scan *segment.DiscoverSegments, ev pipeline.ActivityChanged, logger *slog.Logger) error {
	scanSource := func(id domain.SourceID) error {
		result, err := scan.ForSource(ctx, ev.UserID, id)
		if result.RouteWindows > 0 {
			logger.Info("segment discovery scan finished", "activity_id", ev.ActivityID, "source_id", id,
				"trigger", ev.Kind, "route_windows", result.RouteWindows, "provider_requests", result.ProviderRequests,
				"provider_responses", result.ProviderResponses, "cached_windows", result.CachedWindows,
				"validated_definitions", result.ValidatedDefinitions, "match_candidates", result.CandidatesEvaluated,
				"efforts_written", result.EffortsWritten, "request_limit_reached", result.RequestLimitReached,
				"success", err == nil)
		}
		return err
	}
	switch ev.Kind {
	case pipeline.ChangeIngested:
		return scanSource(ev.SourceID)
	case pipeline.ChangeSegmentRescan:
		act, err := scan.Activities.GetActivity(ctx, ev.ActivityID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if act.UserID != ev.UserID || act.IsDeleted() || act.Type != domain.ActivityTypeRide {
			return nil
		}
		sources, err := scan.Activities.ListSourcesForActivity(ctx, act.ID)
		if err != nil {
			return err
		}
		for _, source := range sources {
			if err := scanSource(source.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
