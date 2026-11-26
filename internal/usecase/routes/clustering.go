package routes

import (
	"context"
	"math"
	"slices"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type cluster struct {
	routes  []domain.RecordedRouteID
	center  port.RouteCandidate
	members []port.RouteCandidate
}

func (g cluster) contains(id domain.ActivityID) bool {
	return slices.ContainsFunc(g.members, func(member port.RouteCandidate) bool { return member.Activity == id })
}

func (g cluster) eligible() bool {
	riders := make(map[domain.UserID]bool, len(g.members))
	for _, member := range g.members {
		if riders[member.Rider] {
			return true
		}
		riders[member.Rider] = true
	}
	return false
}

type activityPair struct {
	first, second domain.ActivityID
}

// Keep the coverage indexes and pair scores for the duration of one job.
// A representative can be tested many times as its group grows.
type clustering struct {
	repo     port.RecordedRouteWriter
	coverage map[domain.ActivityID]domain.RouteCoverage
	scores   map[activityPair]float64
}

func newClustering(repo port.RecordedRouteWriter) *clustering {
	return &clustering{
		repo: repo, coverage: make(map[domain.ActivityID]domain.RouteCoverage), scores: make(map[activityPair]float64),
	}
}

func (c *clustering) track(member port.RouteCandidate) domain.RouteCoverage {
	if coverage, ok := c.coverage[member.Activity]; ok {
		return coverage
	}
	coverage := member.Track.Coverage()
	c.coverage[member.Activity] = coverage
	return coverage
}

func (c *clustering) score(a, b port.RouteCandidate) float64 {
	if a.Activity == b.Activity {
		return 1
	}
	if slices.Compare(b.Activity[:], a.Activity[:]) < 0 {
		a, b = b, a
	}
	key := activityPair{a.Activity, b.Activity}
	if score, ok := c.scores[key]; ok {
		return score
	}
	score := c.track(a).RecordedOverlap(c.track(b))
	c.scores[key] = score
	return score
}

func compareRides(a, b port.RouteCandidate) int {
	if order := a.StartTime.Compare(b.StartTime); order != 0 {
		return order
	}
	return slices.Compare(a.Activity[:], b.Activity[:])
}

type clusterFit struct {
	cluster
	totalOverlap float64
	qualifies    bool
}

func (fit clusterFit) betterThan(other clusterFit) bool {
	if fit.qualifies != other.qualifies {
		return fit.qualifies
	}
	if len(fit.members) != len(other.members) {
		return len(fit.members) > len(other.members)
	}
	if math.Abs(fit.totalOverlap-other.totalOverlap) <= 1e-9 {
		return compareRides(fit.center, other.center) < 0
	}
	return fit.totalOverlap > other.totalOverlap
}

// Select the recorded ride with the highest mean overlap among the rides that
// match every member. If a correction or deletion broke an existing cluster,
// keep the largest viable subset, favoring one that still qualifies.
func (c *clustering) representative(members []port.RouteCandidate, allowSubset bool) cluster {
	var best clusterFit
	scratch := make([]port.RouteCandidate, 0, len(members))
	for _, center := range members {
		matches, sum := c.matchingMembers(center, members, scratch[:0], allowSubset)
		if len(matches) == 0 {
			continue
		}
		group := cluster{center: center, members: matches}
		fit := clusterFit{cluster: group, totalOverlap: sum, qualifies: allowSubset && group.eligible()}
		if fit.betterThan(best) {
			fit.members = append(best.members[:0], matches...)
			best = fit
		}
	}
	return best.cluster
}

func (c *clustering) matchingMembers(center port.RouteCandidate, members, matches []port.RouteCandidate, allowSubset bool) ([]port.RouteCandidate, float64) {
	var sum float64
	for _, member := range members {
		score := c.score(center, member)
		if score >= domain.MinRecordedRouteOverlap {
			matches = append(matches, member)
			sum += score
		} else if !allowSubset {
			return nil, 0
		}
	}
	return matches, sum
}

func (c *clustering) repair(ctx context.Context, route domain.RecordedRouteID) (cluster, error) {
	members, err := c.repo.Members(ctx, route)
	if err != nil {
		return cluster{}, err
	}
	group := c.representative(members, false)
	if len(group.members) == 0 && len(members) != 0 {
		group = c.representative(members, true)
	}
	group.routes = []domain.RecordedRouteID{route}
	if len(group.members) == 0 || len(group.members) < len(members) {
		// Release rides that no longer belong before looking for merges.
		if err := c.save(ctx, group); err != nil || len(group.members) == 0 {
			return group, err
		}
	}
	return c.expand(ctx, group)
}

type neighbor struct {
	candidate port.RouteCandidate
	members   []port.RouteCandidate
}

type neighborhood struct {
	options []neighbor
	routes  map[domain.RecordedRouteID]bool
	rides   map[domain.ActivityID]bool
}

// Explore the spatial neighborhood of every newly added member. A merge may
// use either group's member as its new center. Revisit rejected neighbors
// after adding members because a new center can make them viable.
func (c *clustering) expand(ctx context.Context, group cluster) (cluster, error) {
	found := neighborhood{routes: make(map[domain.RecordedRouteID]bool), rides: make(map[domain.ActivityID]bool)}
	for pending := group.members; len(pending) != 0; {
		if err := c.findNeighbors(ctx, group, pending, &found); err != nil {
			return group, err
		}
		group, pending = c.mergeNext(group, &found)
	}
	if len(group.routes) > 0 || group.eligible() {
		return group, c.save(ctx, group)
	}
	return group, nil
}

func (c *clustering) findNeighbors(ctx context.Context, group cluster, pending []port.RouteCandidate, found *neighborhood) error {
	nearby, err := c.repo.Nearby(ctx, rideIDs(pending), rideIDs(group.members))
	if err != nil {
		return err
	}
	for _, ride := range nearby {
		if found.rides[ride.Activity] || found.routes[ride.Route] || slices.Contains(group.routes, ride.Route) {
			continue
		}
		if !slices.ContainsFunc(pending, func(member port.RouteCandidate) bool {
			return c.score(ride, member) >= domain.MinRecordedRouteOverlap
		}) {
			continue
		}
		found.rides[ride.Activity] = true
		option := neighbor{candidate: ride, members: []port.RouteCandidate{ride}}
		if ride.Route != (domain.RecordedRouteID{}) {
			found.routes[ride.Route] = true
			option.members, err = c.repo.Members(ctx, ride.Route)
			if err != nil {
				return err
			}
		}
		found.options = append(found.options, option)
	}
	return nil
}

func (c *clustering) mergeNext(group cluster, found *neighborhood) (cluster, []port.RouteCandidate) {
	// Keep established groups together when choosing between compatible
	// options. Within each class, use start time and activity ID.
	slices.SortStableFunc(found.options, compareNeighbors)
	for i, option := range found.options {
		if len(option.members) == 0 {
			continue
		}
		union := c.representative(slices.Concat(group.members, option.members), false)
		if len(union.members) == 0 {
			continue
		}
		union.routes = group.routes
		if route := option.candidate.Route; route != (domain.RecordedRouteID{}) {
			union.routes = append(union.routes, route)
		}
		found.options = slices.Delete(found.options, i, i+1)
		return union, option.members
	}
	return group, nil
}

func rideIDs(members []port.RouteCandidate) []domain.ActivityID {
	ids := make([]domain.ActivityID, len(members))
	for i, member := range members {
		ids[i] = member.Activity
	}
	return ids
}

func compareNeighbors(a, b neighbor) int {
	hasRouteA := a.candidate.Route != (domain.RecordedRouteID{})
	hasRouteB := b.candidate.Route != (domain.RecordedRouteID{})
	if hasRouteA != hasRouteB {
		if hasRouteA {
			return -1
		}
		return 1
	}
	return compareRides(a.candidate, b.candidate)
}

func (c *clustering) save(ctx context.Context, group cluster) error {
	// Route IDs are UUIDv7. Keep the oldest ID after a merge.
	slices.SortFunc(group.routes, func(a, b domain.RecordedRouteID) int { return slices.Compare(a[:], b[:]) })
	matches := make([]port.RouteMatch, len(group.members))
	for i, member := range group.members {
		matches[i] = port.RouteMatch{Activity: member.Activity, Overlap: c.score(group.center, member)}
	}
	return c.repo.SaveCluster(ctx, group.routes, group.center.Activity, matches)
}
