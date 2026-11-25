package connect

import (
	"context"
	"fmt"

	connectrpc "connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/opencairn-org/cairn-core/gen/proto/cairn/v1"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func (s *SocialServer) GetProfile(ctx context.Context, req *v1.GetProfileRequest) (*v1.GetProfileResponse, error) {
	if s.Profiles == nil {
		return nil, connectrpc.NewError(connectrpc.CodeUnavailable, "profiles are not available on this instance")
	}
	me, err := userIDFromHandlerCtx(ctx)
	if err != nil {
		return nil, err
	}
	pv, err := s.Profiles.Get(ctx, req.GetUsername(), &me)
	if err != nil {
		return nil, actionErr(err)
	}
	out := &v1.GetProfileResponse{
		User: userCardToProto(pv.User), IsSelf: pv.IsOwner,
	}
	for _, p := range pv.Activities {
		out.Activities = append(out.Activities, s.projectedToProto(ctx, p))
	}
	return out, nil
}

func (s *SocialServer) GetProjectedActivity(ctx context.Context, req *v1.GetProjectedActivityRequest) (*v1.GetProjectedActivityResponse, error) {
	if s.Shares == nil {
		return nil, connectrpc.NewError(connectrpc.CodeUnavailable, "activity projections are not available on this instance")
	}
	me, err := userIDFromHandlerCtx(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseActivityID(req.GetActivityId())
	if err != nil {
		return nil, err
	}
	p, owner, isOwner, err := s.Shares.Projected(ctx, id, &me, false)
	if err != nil {
		return nil, actionErr(err)
	}
	return &v1.GetProjectedActivityResponse{Activity: s.projectedToProto(ctx, p), Owner: userCardToProto(owner), IsOwner: isOwner}, nil
}

func (s *SocialServer) GetSharedActivity(ctx context.Context, req *v1.GetSharedActivityRequest) (*v1.GetSharedActivityResponse, error) {
	if s.Shares == nil {
		return nil, connectrpc.NewError(connectrpc.CodeUnavailable, "activity projections are not available on this instance")
	}
	me, err := userIDFromHandlerCtx(ctx)
	if err != nil {
		return nil, err
	}
	p, owner, err := s.Shares.Shared(ctx, s.ShareLinks, req.GetToken(), &me)
	if err != nil {
		return nil, actionErr(err)
	}
	return &v1.GetSharedActivityResponse{Activity: s.projectedToProto(ctx, p), Owner: userCardToProto(owner)}, nil
}

func (s *SocialServer) ListReviewQueue(ctx context.Context, _ *v1.ListReviewQueueRequest) (*v1.ListReviewQueueResponse, error) {
	me, err := userIDFromHandlerCtx(ctx)
	if err != nil {
		return nil, err
	}
	if s.Activities == nil {
		return nil, connectrpc.NewError(connectrpc.CodeUnavailable, "the review queue is not available on this instance")
	}
	items, err := s.Activities.ListActivitiesNeedingReview(ctx, me)
	if err != nil {
		return nil, connectError(connectrpc.CodeInternal, fmt.Errorf("review queue: %w", err))
	}
	out := &v1.ListReviewQueueResponse{}
	for _, it := range items {
		item := &v1.ReviewItem{Id: it.ID.String(), Title: it.Title, Type: activityTypeToProto(it.Type), StartTime: timestamppb.New(it.StartTime), MatchConfidence: it.MatchConfidence, Kind: string(it.Kind)}
		if it.Kind == domain.ReviewKindDuplicate {
			item.DuplicateOfId = it.DuplicateOf.String()
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// Compile-time use of the port types the server's optional fields hold.
var (
	_ port.ShareLinkRepo = (port.ShareLinkRepo)(nil)
	_                    = domain.ActivityReviewItem{}
)
