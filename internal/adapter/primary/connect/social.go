package connect

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/opencairn-org/cairn-core/gen/proto/cairn/v1"
	"github.com/opencairn-org/cairn-core/gen/proto/cairn/v1/cairnv1connect"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

// SocialServer implements cairnv1connect.SocialServiceHandler: reads across
// users, every non-owner activity projected through the visibility model.
type SocialServer struct {
	cairnv1connect.UnimplementedSocialServiceHandler

	Shares  *view.ActivityShares
	Content port.ContentSigner // optional; nil → no map image URLs

	// Profiles, ShareLinks and Activities back the profile,
	// shared-activity and review-queue reads; nil → Unavailable.
	Profiles   *view.Profiles
	ShareLinks port.ShareLinkRepo
	Activities port.ActivityReviewQueue
}

var _ cairnv1connect.SocialServiceHandler = (*SocialServer)(nil)

// NewSocialServer wires the server.
func NewSocialServer(shares *view.ActivityShares, content port.ContentSigner) *SocialServer {
	return &SocialServer{Shares: shares, Content: content}
}

// projectedToProto converts a projection; the map image is the trimmed
// variant (projections are for non-owners by definition).
func (s *SocialServer) projectedToProto(ctx context.Context, p domain.ProjectedActivity) *v1.ProjectedActivity {
	out := projectedActivityToProto(p)
	if p.MapVisible && s.Content != nil {
		if u, err := s.Content.MapImage(ctx, p.ID, false); err == nil {
			out.MapImage = contentURLToProto(u)
		}
	}
	return out
}

func projectedActivityToProto(p domain.ProjectedActivity) *v1.ProjectedActivity {
	out := &v1.ProjectedActivity{
		Id:                    p.ID.String(),
		Title:                 p.Title,
		Type:                  activityTypeToProto(p.Type),
		Discipline:            string(p.Discipline),
		StartTime:             timestamppb.New(p.StartTime),
		Timezone:              p.Timezone,
		ElapsedDurationS:      int64(p.ElapsedDuration.Seconds()),
		MovingDurationS:       int64(p.MovingDuration.Seconds()),
		DistanceM:             p.DistanceM,
		ElevationGainM:        p.ElevationGainM,
		Description:           p.Description,
		Tss:                   p.TSS,
		Calories:              intPtrToInt32(p.CaloriesKcal),
		StartPlace:            p.StartPlace,
		StartLocationRedacted: p.StartLocationRedacted,
		AvgHr:                 intPtrToInt32(p.AvgHeartRateBpm),
		MaxHr:                 intPtrToInt32(p.MaxHeartRateBpm),
		AvgPower:              intPtrToInt32(p.AvgPowerW),
		MaxPower:              intPtrToInt32(p.MaxPowerW),
		NormalizedPower:       intPtrToInt32(p.NormalizedPowerW),
		AvgSpeedMps:           p.AvgSpeedMps,
		MaxSpeedMps:           p.MaxSpeedMps,
		HasPhotos:             p.PhotosVisible,
	}
	if p.StartLat != nil && p.StartLng != nil {
		out.Start = &v1.GeoPoint{Latitude: *p.StartLat, Longitude: *p.StartLng}
	}
	for _, c := range p.Visible {
		out.VisibleCategories = append(out.VisibleCategories, string(c))
	}
	return out
}

func userCardToProto(u domain.User) *v1.UserCard {
	if u.ID == (domain.UserID{}) {
		return nil
	}
	return &v1.UserCard{Id: u.ID.String(), Username: u.Username, DisplayName: u.DisplayName, AvatarUrl: u.AvatarURL}
}
