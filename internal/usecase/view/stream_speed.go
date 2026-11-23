package view

import (
	"context"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// ApplyCyclingStreamSpeed projects validated speeds into the requested chart
// resolution. Continuous aggregates may otherwise average a GPS jump together
// with valid samples. The raw recording remains available through file export.
func ApplyCyclingStreamSpeed(ctx context.Context, repo port.StreamRepo, stream *domain.Stream) error {
	if len(stream.Samples) == 0 {
		return nil
	}
	raw := *stream
	var interval time.Duration
	switch stream.Resolution {
	case domain.StreamResolution5s:
		interval = 5 * time.Second
	case domain.StreamResolution30s:
		interval = 30 * time.Second
	}
	if interval > 0 {
		var err error
		raw, err = repo.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: stream.ActivitySourceID})
		if err != nil {
			return err
		}
	}
	legs := domain.ValidateCyclingStream(raw.Samples)
	if !legs.HasMotion {
		return nil
	}
	type accumulation struct{ metres, seconds float64 }
	speeds := make(map[time.Time]accumulation)
	for i, leg := range legs.Legs {
		if !leg.Valid || !leg.HasMotion {
			continue
		}
		key := raw.Samples[i].Timestamp.UTC().Truncate(interval)
		current := speeds[key]
		current.metres += leg.DistanceM
		current.seconds += leg.Seconds
		speeds[key] = current
	}
	for i := range stream.Samples {
		sample := &stream.Samples[i]
		average := speeds[sample.Timestamp.UTC().Truncate(interval)]
		sample.SpeedMps = nil
		if average.seconds > 0 {
			sample.SpeedMps = new(average.metres / average.seconds)
		}
	}
	return nil
}
