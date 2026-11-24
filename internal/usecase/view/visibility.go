// Package view holds the read-side use cases shared by the REST and Connect adapters.
package view

import (
	"context"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/streamalign"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Visibility grants authenticated members access to ordinary activities.
type Visibility struct {
	Zones  port.PrivacyZoneRepo
	Blocks port.BlockRepo
}

func NewVisibility() *Visibility { return &Visibility{} }

func memberCanView(act domain.Activity, viewer *domain.UserID) bool {
	return viewer != nil && *viewer != (domain.UserID{}) && !act.IsDeleted() &&
		(*viewer == act.UserID || (!act.HiddenByAdmin && !act.ProviderRestricted))
}

// CanReact applies social blocks to kudos and comments.
func (v *Visibility) CanReact(ctx context.Context, act domain.Activity, viewer domain.UserID) bool {
	if !memberCanView(act, &viewer) {
		return false
	}
	if viewer == act.UserID || v == nil || v.Blocks == nil {
		return true
	}
	blocked, err := v.Blocks.IsBlockedEitherWay(ctx, viewer, act.UserID)
	return err == nil && !blocked
}

// Viewer describes how a member may look at an activity.
type Viewer struct {
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

func (v *Visibility) Resolve(ctx context.Context, act domain.Activity, viewer *domain.UserID) (Viewer, bool) {
	if !memberCanView(act, viewer) {
		return Viewer{}, false
	}
	result := Viewer{viewer: viewer, IsOwner: *viewer == act.UserID}
	if !result.IsOwner && v != nil && v.Zones != nil {
		zones, err := v.Zones.ListPrivacyZones(ctx, act.UserID)
		if err != nil {
			return Viewer{}, false
		}
		result.Zones = zones
	}
	return result, true
}

// Redact masks the start location inside a geographic privacy zone.
func (v Viewer) Redact(act domain.Activity) domain.Activity {
	if act.StartLat != nil && act.StartLng != nil && domain.PointInAnyZone(v.Zones, *act.StartLat, *act.StartLng) {
		act.StartPlace, act.StartLat, act.StartLng = "", nil, nil
	}
	return act
}

func (v Viewer) CanExport() bool { return len(v.Zones) == 0 }

func (v Viewer) RedactMergedStream(stream streamalign.MergedStream) streamalign.MergedStream {
	if len(v.Zones) == 0 {
		return stream
	}
	lat, lon := stream.Channels[domain.StreamChannelLatitude], stream.Channels[domain.StreamChannelLongitude]
	for i := 0; i < min(len(lat), len(lon)); i++ {
		if lat[i] != nil && lon[i] != nil && domain.PointInAnyZone(v.Zones, *lat[i], *lon[i]) {
			for _, channel := range []domain.StreamChannel{
				domain.StreamChannelLatitude, domain.StreamChannelLongitude, domain.StreamChannelAltitude, domain.StreamChannelGrade,
			} {
				if points := stream.Channels[channel]; i < len(points) {
					points[i] = nil
				}
			}
		}
	}
	return stream
}
