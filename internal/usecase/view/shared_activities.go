package view

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type SharedRide struct {
	Activities [2]SharedActivity `json:"activities"`
	Date       time.Time         `json:"date"`
	DistanceM  float64           `json:"distance_m"`
	DurationS  float64           `json:"duration_s"`
	end        time.Time
}

type SharedActivity struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Rider string `json:"rider"`
}

type CrossedPath struct {
	Activities [2]SharedActivity `json:"activities"`
	Time       time.Time         `json:"time"`
	Latitude   float64           `json:"lat"`
	Longitude  float64           `json:"lon"`
}

type SharedActivities struct {
	Rides           []SharedRide  `json:"rides"`
	Encounters      []CrossedPath `json:"encounters"`
	SharedDistanceM float64       `json:"shared_distance_m"`
}

func (h *HeadToHead) Shared(ctx context.Context, viewer domain.UserID, first, second string, from, to time.Time) (SharedActivities, error) {
	out := SharedActivities{Rides: []SharedRide{}, Encounters: []CrossedPath{}}
	if h.SharedRides == nil {
		return out, ErrUnavailable
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return out, port.NewActionError(port.ActionErrorInvalid, "Choose an end date after the start date")
	}
	riders, err := h.riders(ctx, viewer, first, second)
	if err != nil {
		return out, err
	}
	// Use the same calculation and cache entries in either rider order.
	reverse := riders[0].ID.String() > riders[1].ID.String()
	if reverse {
		riders[0], riders[1] = riders[1], riders[0]
	}
	masks, visibility, maskKey, err := h.sharedPrivacy(ctx, viewer, riders)
	if err != nil {
		return out, err
	}
	pairs, err := h.SharedRides.ListSharedRidePairs(ctx, viewer, riders[0].ID, riders[1].ID, from, to,
		visibility, maskKey)
	if err != nil {
		return out, err
	}
	for _, pair := range pairs {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		proximity, err := h.rideProximity(ctx, pair, visibility, masks)
		if err != nil {
			return out, err
		}
		activities := [2]SharedActivity{
			{ID: pair.First.ID.String(), Title: pair.First.Title, Rider: riders[0].Username},
			{ID: pair.Second.ID.String(), Title: pair.Second.Title, Rider: riders[1].Username},
		}
		if reverse {
			activities[0], activities[1] = activities[1], activities[0]
		}
		out.addPair(activities, proximity)
	}
	out.deduplicate(from, to)
	return out, nil
}

func (h *HeadToHead) sharedPrivacy(ctx context.Context, viewer domain.UserID, riders [2]domain.User) ([2][]domain.PrivacyZone, int16, string, error) {
	var masks [2][]domain.PrivacyZone
	var visibility int16
	digest := sha256.New()
	for i, rider := range riders {
		owner := viewer == rider.ID
		if owner {
			visibility = int16(i) + 1 //nolint:gosec // riders has exactly two entries
		}
		zones, err := h.Visibility.zonesForMember(ctx, rider.ID, owner)
		if err != nil {
			return masks, 0, "", fmt.Errorf("load shared ride privacy zones: %w", err)
		}
		masks[i] = zones
		slices.SortFunc(zones, func(a, b domain.PrivacyZone) int {
			return cmp.Compare(a.ID.String(), b.ID.String())
		})
		_, _ = fmt.Fprintf(digest, "%d:", i)
		for _, zone := range zones {
			_, _ = fmt.Fprintf(digest, "%g,%g,%g;", zone.Lat, zone.Lng, zone.RadiusM)
		}
	}
	return masks, visibility, hex.EncodeToString(digest.Sum(nil)), nil
}

func (h *HeadToHead) rideProximity(ctx context.Context, pair port.SharedRidePair, visibility int16, masks [2][]domain.PrivacyZone) (domain.RideProximity, error) {
	if pair.Cached != nil {
		return *pair.Cached, nil
	}
	a, err := h.SharedRides.ReadSharedRideTrack(ctx, pair.First)
	if err != nil {
		return domain.RideProximity{}, err
	}
	b, err := h.SharedRides.ReadSharedRideTrack(ctx, pair.Second)
	if err != nil {
		return domain.RideProximity{}, err
	}
	result := domain.CompareRideTracks(a, b, masks[0], masks[1])
	return result, h.SharedRides.SaveSharedRidePair(ctx, pair, visibility, result)
}

func (out *SharedActivities) addPair(activities [2]SharedActivity, proximity domain.RideProximity) {
	if ride := proximity.Shared; ride != nil {
		out.Rides = append(out.Rides, SharedRide{
			Activities: activities, Date: ride.Start, DistanceM: ride.DistanceM, DurationS: ride.Seconds, end: ride.End,
		})
	}
	for _, encounter := range proximity.Encounters {
		out.Encounters = append(out.Encounters, CrossedPath{
			Activities: activities, Time: encounter.Time, Latitude: encounter.Location.Lat, Longitude: encounter.Location.Lon,
		})
	}
}

func inSharedDateRange(at, from, to time.Time) bool {
	return (from.IsZero() || !at.Before(from)) && (to.IsZero() || at.Before(to))
}

func (out *SharedActivities) deduplicate(from, to time.Time) {
	unique := deduplicateSharedRides(out.Rides)
	out.Encounters = deduplicateEncounters(out.Encounters, unique, from, to)
	out.Rides = slices.DeleteFunc(unique, func(ride SharedRide) bool { return !inSharedDateRange(ride.Date, from, to) })
	for _, ride := range out.Rides {
		out.SharedDistanceM += ride.DistanceM
	}
	// Keep the ride list newest first and encounters in chronological order.
	slices.Reverse(out.Rides)
}

func deduplicateSharedRides(rides []SharedRide) []SharedRide {
	slices.SortStableFunc(rides, func(a, b SharedRide) int { return a.Date.Compare(b.Date) })
	// Unmerged duplicate recordings can produce multiple overlapping matches
	// for the same two riders. Keep the most complete detected recording pair.
	unique := rides[:0]
	for start := 0; start < len(rides); {
		best, last := rides[start], rides[start].end
		end := start + 1
		for end < len(rides) && rides[end].Date.Before(last) {
			ride := rides[end]
			if ride.DurationS > best.DurationS {
				best = ride
			}
			if ride.end.After(last) {
				last = ride.end
			}
			end++
		}
		unique = append(unique, best)
		start = end
	}
	return unique
}

func deduplicateEncounters(encounters []CrossedPath, rides []SharedRide, from, to time.Time) []CrossedPath {
	slices.SortStableFunc(encounters, func(a, b CrossedPath) int { return a.Time.Compare(b.Time) })
	events := encounters[:0]
	ride := 0
	for _, event := range encounters {
		if !inSharedDateRange(event.Time, from, to) {
			continue
		}
		for ride < len(rides) && rides[ride].end.Before(event.Time) {
			ride++
		}
		if ride < len(rides) && !event.Time.Before(rides[ride].Date) {
			continue
		}
		if n := len(events); n > 0 && sameEncounter(event, events[n-1]) {
			continue
		}
		events = append(events, event)
	}
	return events
}

func sameEncounter(first, second CrossedPath) bool {
	return first.Time.Sub(second.Time) <= 15*time.Second &&
		domain.HaversineMeters(domain.GeoPoint{Lat: first.Latitude, Lon: first.Longitude},
			domain.GeoPoint{Lat: second.Latitude, Lon: second.Longitude}) <= 30
}
