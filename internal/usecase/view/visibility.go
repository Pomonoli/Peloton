// Package view holds the read-side use cases shared by the REST and Connect adapters.
package view

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/streamalign"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Visibility resolves the activity's audience, explicit policy and privacy zones.
type Visibility struct {
	Policies port.VisibilityRepo
	Follows  port.FollowRepo
	Blocks   port.BlockRepo
}

func NewVisibility() *Visibility { return &Visibility{} }

func (v *Visibility) For(ctx context.Context, act *domain.Activity, viewer *domain.UserID, hasLink bool) domain.CategorySet {
	if act.IsDeleted() {
		return nil
	}
	in := domain.VisibilityInputFor(act)
	in.IsOwner = viewer != nil && *viewer == act.UserID
	if in.IsOwner {
		return domain.ResolveVisibility(in)
	}
	in.HasValidLink = hasLink
	if v != nil {
		if viewer != nil && v.Blocks != nil {
			blocked, err := v.Blocks.IsBlockedEitherWay(ctx, *viewer, act.UserID)
			if err != nil || blocked {
				return nil
			}
		}
		if viewer != nil && !hasLink && v.Follows != nil {
			follows, err := v.Follows.IsFollowing(ctx, *viewer, act.UserID)
			if err != nil {
				return nil
			}
			in.IsFollower = follows
		}
		if v.Policies != nil {
			policy, err := v.Policies.GetUserPolicy(ctx, act.UserID)
			if err != nil {
				return nil
			}
			override, err := v.Policies.GetActivityOverride(ctx, act.ID)
			if err != nil {
				return nil
			}
			in.OwnerPolicy, in.ActivityPolicy = policy, override
		}
	}
	return domain.ResolveVisibility(in)
}

// Viewer describes how a member may look at an activity.
type Viewer struct {
	Cats    domain.CategorySet
	Zones   []domain.PrivacyZone
	IsOwner bool
	viewer  *domain.UserID
}

func (v Viewer) ActivityReadContext(ctx context.Context) context.Context {
	if v.viewer != nil {
		ctx = port.WithCommunityViewer(ctx, *v.viewer)
	}
	return port.WithActivityReadViewer(ctx, v.IsOwner)
}

func (v *Visibility) Resolve(ctx context.Context, act domain.Activity, viewer *domain.UserID, hasLink bool) (Viewer, bool) {
	cats := v.For(ctx, &act, viewer, hasLink)
	if len(cats) == 0 {
		return Viewer{}, false
	}
	result := Viewer{Cats: cats, viewer: viewer, IsOwner: viewer != nil && *viewer == act.UserID}
	if !result.IsOwner && v != nil && v.Policies != nil && (cats.Has(domain.CategoryLocation) || cats.Has(domain.CategoryMap)) {
		zones, err := v.Policies.ListPrivacyZones(ctx, act.UserID)
		if err != nil {
			return Viewer{}, false
		}
		result.Zones = zones
	}
	return result, true
}

// Redact applies category restrictions to the activity returned by detailed
// read APIs while preserving the underlying record for repository operations.
func (v Viewer) Redact(act domain.Activity) domain.Activity {
	if v.IsOwner {
		return act
	}
	projected := domain.ProjectActivity(act, v.Cats, v.Zones)
	act.Description, act.StartPlace, act.StartLat, act.StartLng = projected.Description, projected.StartPlace, projected.StartLat, projected.StartLng
	act.Summary.AvgHeartRateBpm, act.Summary.MaxHeartRateBpm = projected.AvgHeartRateBpm, projected.MaxHeartRateBpm
	act.Summary.AvgPowerW, act.Summary.MaxPowerW, act.Summary.NormalizedPowerW = projected.AvgPowerW, projected.MaxPowerW, projected.NormalizedPowerW
	act.Summary.AvgSpeedMps, act.Summary.MaxSpeedMps = projected.AvgSpeedMps, projected.MaxSpeedMps
	act.Summary.TSS, act.Summary.CaloriesKcal = projected.TSS, projected.CaloriesKcal
	if !v.Cats.Has(domain.CategoryPower) {
		act.Summary.IntensityFactor = nil
	}
	if !v.Cats.Has(domain.CategoryCadence) {
		act.Summary.AvgCadence, act.Summary.MaxCadence = nil, nil
	}
	return act
}

func (v Viewer) CanExport() bool {
	return v.IsOwner || (len(v.Zones) == 0 && len(v.Cats) == len(domain.AllCategories))
}

func (v Viewer) RedactLap(lap domain.ActivityLap) domain.ActivityLap {
	if !v.Cats.Has(domain.CategoryHR) {
		lap.AvgHeartRateBpm = nil
	}
	if !v.Cats.Has(domain.CategoryPower) {
		lap.AvgPowerW = nil
	}
	if !v.Cats.Has(domain.CategoryCadence) {
		lap.AvgCadence = nil
	}
	if !v.Cats.Has(domain.CategoryPaceSpeed) {
		lap.AvgSpeedMps = nil
	}
	return lap
}

func (v Viewer) RedactEffort(effort domain.SegmentEffort) domain.SegmentEffort {
	if !v.Cats.Has(domain.CategoryHR) {
		effort.AvgHeartRateBpm, effort.MaxHeartRateBpm = nil, nil
	}
	if !v.Cats.Has(domain.CategoryPower) {
		effort.AvgPowerW = nil
	}
	if !v.Cats.Has(domain.CategoryCadence) {
		effort.AvgCadence = nil
	}
	return effort
}

func (v Viewer) RedactZones(zones domain.ZonesResult) domain.ZonesResult {
	if !v.Cats.Has(domain.CategoryZones) {
		return domain.ZonesResult{}
	}
	if !v.Cats.Has(domain.CategoryHR) {
		zones.HR, zones.Decoupling = nil, nil
	}
	if !v.Cats.Has(domain.CategoryPower) {
		zones.Power, zones.Decoupling = nil, nil
	}
	return zones
}

func (v Viewer) RedactMergedStream(stream streamalign.MergedStream) streamalign.MergedStream {
	if v.IsOwner {
		return stream
	}
	lat, lon := stream.Channels[domain.StreamChannelLatitude], stream.Channels[domain.StreamChannelLongitude]
	var mask []int
	if len(v.Zones) > 0 {
		for i := 0; i < min(len(lat), len(lon)); i++ {
			if lat[i] != nil && lon[i] != nil && domain.PointInAnyZone(v.Zones, *lat[i], *lon[i]) {
				mask = append(mask, i)
			}
		}
	}
	for channel, points := range stream.Channels {
		if !v.Cats.Has(domain.StreamCategory(channel)) {
			delete(stream.Channels, channel)
			delete(stream.Provenance, channel)
		} else if len(v.Zones) > 0 && domain.StreamCategory(channel) == domain.CategoryMap {
			for _, i := range mask {
				if i < len(points) {
					points[i] = nil
				}
			}
		}
	}
	return stream
}
