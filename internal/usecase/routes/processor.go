package routes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type Processor struct {
	Repo    port.RecordedRouteWriter
	Streams port.StreamRepo
	Tx      port.TxManager
}

// ProcessOne handles one durable job. Serializing catalog writes across server
// processes prevents simultaneous discoveries of the same route. A generation
// check ensures a concurrent import or correction cannot lose its queue entry.
func (p *Processor) ProcessOne(ctx context.Context) (bool, error) {
	var job port.RouteJob
	var worked bool
	err := p.Tx.InTx(ctx, func(ctx context.Context) error {
		locked, err := p.Repo.Lock(ctx)
		if err != nil || !locked {
			return err
		}
		worked, err = p.Repo.EnqueueRecluster(ctx)
		if err != nil || worked {
			return err
		}
		job, err = p.Repo.Next(ctx)
		if err != nil {
			return err
		}
		if job.Activity == (domain.ActivityID{}) {
			route, err := p.Repo.Orphan(ctx)
			if err != nil || route == (domain.RecordedRouteID{}) {
				return err
			}
			worked = true
			_, err = newClustering(p.Repo).repair(ctx, route)
			return err
		}
		worked = true
		if err := p.process(ctx, job.Activity); err != nil {
			return err
		}
		return p.Repo.Finish(ctx, job)
	})
	if err != nil && job.Activity != (domain.ActivityID{}) && ctx.Err() == nil {
		err = errors.Join(err, p.Repo.Retry(ctx, job))
		return worked, fmt.Errorf("process route for %s: %w", job.Activity, err)
	}
	return worked, err
}

func (p *Processor) process(ctx context.Context, id domain.ActivityID) error {
	prior, err := p.Repo.Membership(ctx, id)
	if err != nil {
		return err
	}
	valid, err := p.refreshTrack(ctx, id)
	if err != nil {
		return err
	}

	clustering := newClustering(p.Repo)
	if prior != (domain.RecordedRouteID{}) {
		group, err := clustering.repair(ctx, prior)
		if err != nil || group.contains(id) {
			return err
		}
	}
	if !valid {
		return nil
	}
	candidate, err := p.Repo.Candidate(ctx, id)
	if err != nil || len(candidate.Track) == 0 {
		return err
	}
	_, err = clustering.expand(ctx, cluster{center: candidate, members: []port.RouteCandidate{candidate}})
	return err
}

func (p *Processor) refreshTrack(ctx context.Context, id domain.ActivityID) (bool, error) {
	input, err := p.Repo.Input(ctx, id)
	if err != nil {
		return false, err
	}
	old, err := p.Repo.Track(ctx, id)
	if err != nil {
		return false, err
	}
	current, changed, err := p.loadTrack(ctx, old, input)
	valid := current.LengthM > 0
	if err != nil || !changed {
		return valid, err
	}
	if valid {
		return true, p.Repo.SaveTrack(ctx, current)
	}
	if old.LengthM > 0 {
		return false, p.Repo.DeleteTrack(ctx, id)
	}
	return false, nil
}

func (p *Processor) loadTrack(ctx context.Context, old port.RouteTrack, input port.RouteInput) (port.RouteTrack, bool, error) {
	if !input.Shared || input.Source == (domain.SourceID{}) {
		return port.RouteTrack{Activity: old.Activity}, true, nil
	}
	if old.Source == input.Source && old.Revision == input.Revision {
		return old, false, nil
	}
	stream, err := p.Streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: input.Source})
	if err != nil {
		return port.RouteTrack{}, false, err
	}
	track, length := domain.NewRecordedTrack(stream.Samples)
	current := port.RouteTrack{Activity: old.Activity, Source: input.Source, Revision: input.Revision, Track: track, LengthM: length}
	if length > 0 {
		current.Kind = track.Kind(length, track.Coverage())
	}
	return current, true, nil
}

// Run drains short transactions and yields between jobs so a historical
// backlog can share the database with the rest of the application.
func (p *Processor) Run(ctx context.Context, log *slog.Logger) {
	for ctx.Err() == nil {
		worked, err := p.ProcessOne(ctx)
		delay := 20 * time.Millisecond
		if !worked {
			delay = 5 * time.Second
		}
		if err != nil && ctx.Err() == nil {
			log.Warn("recorded route processing", "error", err)
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
		}
	}
}
