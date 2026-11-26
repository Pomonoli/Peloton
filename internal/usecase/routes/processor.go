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
		route, anchor, err := p.Repo.Orphan(ctx)
		if err != nil {
			return err
		}
		if anchor != (domain.ActivityID{}) {
			worked = true
			return p.reanchor(ctx, route, anchor)
		}
		job, err = p.Repo.Next(ctx)
		if err != nil || job.Activity == (domain.ActivityID{}) {
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

type comparison struct {
	kind     domain.RecordedRouteKind
	coverage domain.RouteCoverage
}

func (c comparison) score(candidate port.RouteCandidate) float64 {
	// When both tracks close, distinguish a retraced ride from a circuit.
	if c.kind != candidate.Kind && c.kind != domain.RecordedPointToPoint && candidate.Kind != domain.RecordedPointToPoint {
		return 0
	}
	return c.coverage.RecordedOverlap(candidate.Track.Coverage())
}

func (p *Processor) process(ctx context.Context, id domain.ActivityID) error {
	input, err := p.Repo.Input(ctx, id)
	if err != nil {
		return err
	}
	old, err := p.Repo.Track(ctx, id)
	if err != nil {
		return err
	}
	member, err := p.Repo.Membership(ctx, id)
	if err != nil {
		return err
	}

	current, changed, err := p.loadTrack(ctx, old, input)
	if err != nil {
		return err
	}
	return p.reconcileTrack(ctx, old, current, changed, input.Rider, member)
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

func (p *Processor) reconcileTrack(ctx context.Context, old, current port.RouteTrack, changed bool, rider domain.UserID, member port.RouteMembership) error {
	valid := current.LengthM > 0
	if member.Anchor {
		keep, err := p.canKeepAnchor(ctx, old, current, changed)
		if err != nil {
			return err
		}
		if !keep {
			if err := p.replaceAnchor(ctx, member.Route, current.Activity); err != nil {
				return err
			}
			member = port.RouteMembership{}
		}
	}
	if !valid {
		if old.LengthM > 0 {
			return p.Repo.DeleteTrack(ctx, current.Activity)
		}
		return nil
	}
	if changed {
		if err := p.Repo.SaveTrack(ctx, current); err != nil {
			return err
		}
	}
	if member.Anchor {
		return p.refreshMatches(ctx, member.Route, current)
	}
	return p.match(ctx, current, rider, member.Route)
}

func (p *Processor) canKeepAnchor(ctx context.Context, old, current port.RouteTrack, changed bool) (bool, error) {
	if current.LengthM == 0 || !changed {
		return current.LengthM > 0, nil
	}
	ref := comparison{current.Kind, current.Track.Coverage()}
	if ref.score(port.RouteCandidate{Track: old.Track, Kind: old.Kind}) < domain.MinRecordedRouteOverlap {
		return false, nil
	}
	matched, _, err := p.bestAnchor(ctx, current.Activity, ref)
	return matched == (domain.RecordedRouteID{}), err
}

func (p *Processor) replaceAnchor(ctx context.Context, route domain.RecordedRouteID, previous domain.ActivityID) error {
	candidates, err := p.Repo.Candidates(ctx, previous, port.RouteCandidates{MembersOf: route})
	if err != nil {
		return err
	}
	if err := p.Repo.RemoveMatch(ctx, previous); err != nil {
		return err
	}
	if len(candidates) == 0 {
		return p.Repo.SetAnchor(ctx, route, domain.ActivityID{})
	}
	return p.reanchor(ctx, route, candidates[0].Activity)
}

func (p *Processor) reanchor(ctx context.Context, route domain.RecordedRouteID, anchor domain.ActivityID) error {
	track, err := p.Repo.Track(ctx, anchor)
	if err != nil {
		return err
	}
	// If a replacement coincides with another catalog route, dissolve the old
	// group and requeue each ride for direct comparison with the surviving anchor.
	ref := comparison{track.Kind, track.Track.Coverage()}
	other, _, err := p.bestAnchor(ctx, anchor, ref)
	if err != nil {
		return err
	}
	if other != (domain.RecordedRouteID{}) {
		if err := p.Repo.SetAnchor(ctx, route, domain.ActivityID{}); err != nil {
			return err
		}
		return p.Repo.RetainMatches(ctx, route, nil)
	}
	if err := p.Repo.SetAnchor(ctx, route, anchor); err != nil {
		return err
	}
	return p.refreshMatches(ctx, route, track)
}

func (p *Processor) refreshMatches(ctx context.Context, route domain.RecordedRouteID, anchor port.RouteTrack) error {
	ref := comparison{anchor.Kind, anchor.Track.Coverage()}
	candidates, err := p.Repo.Candidates(ctx, anchor.Activity, port.RouteCandidates{MembersOf: route})
	if err != nil {
		return err
	}
	if err := p.Repo.SetMatch(ctx, anchor.Activity, route, 1); err != nil {
		return err
	}
	retain := []domain.ActivityID{anchor.Activity}
	for _, candidate := range candidates {
		if score := ref.score(candidate); score >= domain.MinRecordedRouteOverlap {
			if err := p.Repo.SetMatch(ctx, candidate.Activity, route, score); err != nil {
				return err
			}
			retain = append(retain, candidate.Activity)
		}
	}
	if err := p.Repo.RetainMatches(ctx, route, retain); err != nil {
		return err
	}
	return p.attachHistory(ctx, route, anchor.Activity, ref)
}

func (p *Processor) match(ctx context.Context, current port.RouteTrack, rider domain.UserID, prior domain.RecordedRouteID) error {
	ref := comparison{current.Kind, current.Track.Coverage()}
	if prior != (domain.RecordedRouteID{}) {
		handled, err := p.matchPrior(ctx, current.Activity, prior, ref)
		if err != nil || handled {
			return err
		}
	}

	anchor, score, err := p.bestAnchor(ctx, current.Activity, ref)
	if err != nil {
		return err
	}
	if anchor != (domain.RecordedRouteID{}) {
		return p.Repo.SetMatch(ctx, current.Activity, anchor, score)
	}
	return p.discoverRoute(ctx, current.Activity, rider, ref)
}

func (p *Processor) matchPrior(ctx context.Context, activity domain.ActivityID, prior domain.RecordedRouteID, ref comparison) (bool, error) {
	anchor, err := p.Repo.Anchor(ctx, prior)
	if err != nil || len(anchor.Track) == 0 {
		// A queued correction or orphan repair will reconcile an inactive
		// anchor. Avoid discovering a duplicate while it is pending.
		return true, err
	}
	if score := ref.score(anchor); score >= domain.MinRecordedRouteOverlap {
		return true, p.Repo.SetMatch(ctx, activity, prior, score)
	}
	return false, p.Repo.RemoveMatch(ctx, activity)
}

func (p *Processor) discoverRoute(ctx context.Context, activity domain.ActivityID, rider domain.UserID, ref comparison) error {
	peers, err := p.Repo.Candidates(ctx, activity, port.RouteCandidates{Unassigned: true, Rider: rider})
	if err != nil {
		return err
	}
	for _, peer := range peers {
		if ref.score(peer) < domain.MinRecordedRouteOverlap {
			continue
		}
		// Earlier unassigned rides make a stable representative during backfill.
		peerRef := comparison{peer.Kind, peer.Track.Coverage()}
		existing, _, err := p.bestAnchor(ctx, peer.Activity, peerRef)
		if err != nil {
			return err
		}
		if existing == (domain.RecordedRouteID{}) {
			return p.createRoute(ctx, peer.Activity, peerRef)
		}
	}
	return nil
}

func (p *Processor) createRoute(ctx context.Context, activity domain.ActivityID, ref comparison) error {
	route, err := p.Repo.Create(ctx, activity)
	if err != nil {
		return err
	}
	if err := p.Repo.SetMatch(ctx, activity, route, 1); err != nil {
		return err
	}
	return p.attachHistory(ctx, route, activity, ref)
}

func (p *Processor) bestAnchor(ctx context.Context, activity domain.ActivityID, ref comparison) (domain.RecordedRouteID, float64, error) {
	candidates, err := p.Repo.Candidates(ctx, activity, port.RouteCandidates{Anchors: true})
	var best domain.RecordedRouteID
	var bestScore float64
	for _, candidate := range candidates {
		if score := ref.score(candidate); score >= domain.MinRecordedRouteOverlap && score > bestScore {
			best, bestScore = candidate.Route, score
		}
	}
	return best, bestScore, err
}

func (p *Processor) attachHistory(ctx context.Context, route domain.RecordedRouteID, anchor domain.ActivityID, ref comparison) error {
	candidates, err := p.Repo.Candidates(ctx, anchor, port.RouteCandidates{Unassigned: true})
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if score := ref.score(candidate); score >= domain.MinRecordedRouteOverlap {
			if err := p.Repo.SetMatch(ctx, candidate.Activity, route, score); err != nil {
				return err
			}
		}
	}
	return nil
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
