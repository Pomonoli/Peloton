package connect

import (
	"context"
	"fmt"
	"math"

	connectrpc "connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/opencairn-org/cairn-core/gen/proto/cairn/v1"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/streamalign"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

// ---------------------------------------------------------------------------
// Per-activity detail reads (kudos/comments gated on the social
// category). All rendered from usecase/view.Details, like the REST routes.
// ---------------------------------------------------------------------------

func (s *ActivityServer) details() (*view.Details, error) {
	if s.Details == nil {
		return nil, connectrpc.NewError(connectrpc.CodeUnavailable, "activity details are not available on this instance")
	}
	return s.Details, nil
}

func (s *ActivityServer) ListLaps(ctx context.Context, req *v1.ListLapsRequest) (*v1.ListLapsResponse, error) {
	d, err := s.details()
	if err != nil {
		return nil, err
	}
	act, _, err := s.visibleActivity(ctx, req.GetActivityId())
	if err != nil {
		return nil, err
	}
	laps, sid, err := d.Laps(ctx, act)
	if err != nil {
		return nil, actionErr(err)
	}
	out := &v1.ListLapsResponse{Laps: make([]*v1.ActivityLap, 0, len(laps))}
	if sid != nil {
		out.SourceId = sid.String()
	}
	for i, l := range laps {
		out.Laps = append(out.Laps, lapToProto(l, i))
	}
	return out, nil
}

func lapToProto(l domain.ActivityLap, pos int) *v1.ActivityLap {
	idx := l.Index
	if idx == 0 {
		idx = pos + 1
	}
	return &v1.ActivityLap{
		Index:           connectClampInt32(idx),
		ElapsedDuration: durationpb.New(l.ElapsedDuration),
		MovingDuration:  durationpb.New(l.MovingDuration),
		DistanceM:       l.DistanceM,
		AvgHeartRateBpm: intPtrToInt32(l.AvgHeartRateBpm),
		AvgPowerW:       intPtrToInt32(l.AvgPowerW),
		AvgSpeedMps:     l.AvgSpeedMps,
		AvgCadence:      intPtrToInt32(l.AvgCadence),
		ElevationGainM:  l.ElevationGainM,
		ElevationLossM:  l.ElevationLossM,
		Label:           l.Label,
		StartOffsetS:    int64(l.StartOffset.Seconds()),
	}
}

func (s *ActivityServer) GetActivityZones(ctx context.Context, req *v1.GetActivityZonesRequest) (*v1.GetActivityZonesResponse, error) {
	d, err := s.details()
	if err != nil {
		return nil, err
	}
	act, viewer, err := s.visibleActivity(ctx, req.GetActivityId())
	if err != nil {
		return nil, err
	}
	z, err := d.Zones(ctx, act)
	if err != nil {
		return nil, actionErr(err)
	}
	out := &v1.GetActivityZonesResponse{Hr: zoneSummaryToProto(z.HR, viewer.IsOwner), Power: zoneSummaryToProto(z.Power, viewer.IsOwner)}
	if z.Decoupling != nil {
		out.Decoupling = &v1.Decoupling{Pct: z.Decoupling.Pct, Basis: z.Decoupling.Basis, Coupled: z.Decoupling.Coupled}
	}
	return out, nil
}

func zoneSummaryToProto(z *domain.ZoneSummary, includeReference bool) *v1.ZoneSummary {
	if z == nil {
		return nil
	}
	out := &v1.ZoneSummary{Basis: z.Basis, TotalSeconds: connectClampInt32(z.TotalSeconds)}
	if includeReference {
		out.Reference = z.Reference
	}
	for _, b := range z.Zones {
		out.Zones = append(out.Zones, &v1.ZoneBand{Label: b.Label, LowPct: b.LowPct, HighPct: b.HighPct, Seconds: connectClampInt32(b.Seconds)})
	}
	return out
}

func (s *ActivityServer) ListActivitySegmentEfforts(ctx context.Context, req *v1.ListActivitySegmentEffortsRequest) (*v1.ListActivitySegmentEffortsResponse, error) {
	d, err := s.details()
	if err != nil {
		return nil, err
	}
	act, viewer, err := s.visibleActivity(ctx, req.GetActivityId())
	if err != nil {
		return nil, err
	}
	rows, err := d.SegmentEfforts(viewer.ActivityReadContext(ctx), act)
	if err != nil {
		return nil, actionErr(err)
	}
	out := &v1.ListActivitySegmentEffortsResponse{Efforts: make([]*v1.ActivitySegmentEffort, 0, len(rows))}
	for _, row := range rows {
		out.Efforts = append(out.Efforts, effortRowToProto(row))
	}
	return out, nil
}

func effortRowToProto(row view.EffortRow) *v1.ActivitySegmentEffort {
	e, seg := row.Effort, row.Segment
	out := &v1.ActivitySegmentEffort{
		SegmentId:        e.SegmentID.String(),
		SegmentName:      seg.Name,
		SegmentDistanceM: seg.DistanceM,
		ElapsedS:         e.ElapsedS,
		MovingS:          e.MovingS,
		AvgHeartRate:     intPtrToInt32(e.AvgHeartRateBpm),
		AvgPower:         intPtrToInt32(e.AvgPowerW),
		IsPersonalRecord: e.IsPersonalRecord,
		PersonalRank:     connectClampInt32(e.PersonalRank),
		StartOffsetS:     row.StartOffsetS,
		EndOffsetS:       row.EndOffsetS,
		StartDistanceM:   row.StartDistanceM,
		EndDistanceM:     row.EndDistanceM,
	}
	if seg.ClimbCategory != "" && seg.ClimbCategory != domain.ClimbCategoryNone {
		c := string(seg.ClimbCategory)
		out.ClimbCategory = &c
	}
	return out
}

func (s *ActivityServer) ListSimilarActivities(ctx context.Context, req *v1.ListSimilarActivitiesRequest) (*v1.ListSimilarActivitiesResponse, error) {
	d, err := s.details()
	if err != nil {
		return nil, err
	}
	act, viewer, err := s.visibleActivity(ctx, req.GetActivityId())
	if err != nil {
		return nil, err
	}
	g, err := d.Similar(viewer.ActivityReadContext(ctx), act)
	if err != nil {
		return nil, actionErr(err)
	}
	out := &v1.ListSimilarActivitiesResponse{
		TargetSegments: connectClampInt32(g.TargetSegments),
		Count:          connectClampInt32(len(g.Rows)),
		BestMovingS:    g.BestMovingS,
		CurrentMovingS: g.CurrentMovingS,
		CurrentRank:    connectClampInt32(g.CurrentRank),
		IsBest:         g.IsBest,
	}
	for _, row := range g.Rows {
		out.Activities = append(out.Activities, &v1.SimilarActivity{
			Id:             row.ActivityID.String(),
			IsCurrent:      row.IsCurrent,
			Title:          row.Title,
			Type:           activityTypeToProto(domain.ActivityType(row.Type)),
			StartTime:      timestamppb.New(row.StartTime),
			DistanceM:      row.DistanceM,
			MovingS:        row.MovingS,
			ElapsedS:       row.ElapsedS,
			AvgHeartRate:   intPtrToInt32(row.AvgHeartRateBpm),
			AvgPower:       intPtrToInt32(row.AvgPowerW),
			PaceSPerKm:     row.PaceSPerKm,
			SharedSegments: connectClampInt32(row.SharedSegments),
			OverlapPct:     row.OverlapPct,
		})
	}
	return out, nil
}

func (s *ActivityServer) GetMergedStream(ctx context.Context, req *v1.GetMergedStreamRequest) (*v1.GetMergedStreamResponse, error) {
	d, err := s.details()
	if err != nil {
		return nil, err
	}
	act, viewer, err := s.visibleActivity(ctx, req.GetActivityId())
	if err != nil {
		return nil, err
	}
	merged, has, err := d.MergedStream(viewer.ActivityReadContext(ctx), act)
	if err != nil {
		return nil, actionErr(err)
	}
	out := &v1.GetMergedStreamResponse{Channels: map[string]*v1.MergedChannel{}}
	if !has {
		return out, nil
	}
	return mergedStreamToProto(viewer.RedactMergedStream(merged)), nil
}

func mergedStreamToProto(m streamalign.MergedStream) *v1.GetMergedStreamResponse {
	out := &v1.GetMergedStreamResponse{GridUnixS: make([]int64, len(m.Grid)), Channels: map[string]*v1.MergedChannel{}}
	for i, t := range m.Grid {
		out.GridUnixS[i] = t.Unix()
	}
	for ch, vals := range m.Channels {
		mc := &v1.MergedChannel{Values: make([]float64, len(vals))}
		for i, v := range vals {
			if v == nil {
				mc.Values[i] = math.NaN()
			} else {
				mc.Values[i] = *v
			}
		}
		if sid, ok := m.Provenance[ch]; ok {
			mc.SourceId = sid.String()
		}
		out.Channels[string(ch)] = mc
	}
	return out
}

func (s *ActivityServer) GetActivityManageInfo(ctx context.Context, req *v1.GetActivityManageInfoRequest) (*v1.GetActivityManageInfoResponse, error) {
	d, err := s.details()
	if err != nil {
		return nil, err
	}
	act, viewer, err := s.visibleActivity(ctx, req.GetActivityId())
	if err != nil {
		return nil, err
	}
	info, err := d.Manage(viewer.ActivityReadContext(ctx), act)
	if err != nil {
		return nil, connectError(connectrpc.CodeInternal, fmt.Errorf("manage info: %w", err))
	}
	out := &v1.GetActivityManageInfoResponse{MergedAt: timestamppb.New(act.MergedAt)}
	if info.Primary != nil {
		out.PrimarySourceId = info.Primary.String()
	}
	for _, in := range info.Sources {
		out.Sources = append(out.Sources, sourceInsightToProto(in))
	}
	for _, p := range info.Provenance {
		out.Provenance = append(out.Provenance, &v1.FieldProvenance{
			Field: string(p.Field), FieldLabel: p.FieldLabel, SourceId: p.SourceID.String(), Provider: p.Provider, DecidedBy: p.DecidedBy,
			SyncedAt: timestamppb.New(act.MergedAt),
		})
	}
	return out, nil
}

func sourceInsightToProto(in domain.SourceInsight) *v1.SourceInsight {
	s := in.Source
	out := &v1.SourceInsight{
		Source:          sourceToProto(s),
		WorkerRelease:   s.SourceWorkerRelease,
		ParserVersion:   in.ParserVersion,
		DatasetVersion:  in.DatasetVersion,
		ReparseWith:     in.ReparseWith,
		Upgrade:         in.Upgrade,
		WorkerPackage:   s.SourceWorkerPackage,
		ReparseEligible: in.ReparseEligible,
		HasBlob:         s.RawBlobID != "",
		RawContentType:  s.RawContentType,
		RawSizeBytes:    s.RawSizeBytes,
		HasStream:       s.Parsed.HasStream,
		LapCount:        connectClampInt32(len(s.Parsed.Laps)),
		StatusReason:    s.StatusReason,
		ReimportReason:  s.ReimportStatusReason,
		IsPrimary:       in.IsPrimary,
		WonFieldGroups:  in.WonFieldGroups,
	}
	if s.LastReimportedAt != nil {
		out.LastReimportedAt = timestamppb.New(*s.LastReimportedAt)
	}
	return out
}

func (s *ActivityServer) GetParsedSource(ctx context.Context, req *v1.GetParsedSourceRequest) (*v1.GetParsedSourceResponse, error) {
	d, err := s.details()
	if err != nil {
		return nil, err
	}
	act, err := s.activityUpdateLoad(ctx, req.GetActivityId())
	if err != nil {
		return nil, err
	}
	sid, err := parseSourceID(req.GetSourceId())
	if err != nil {
		return nil, err
	}
	src, err := d.ParsedSource(ctx, act, sid)
	if err != nil {
		return nil, notFoundErr(err)
	}
	return &v1.GetParsedSourceResponse{Payload: payloadToProto(src.Parsed)}, nil
}

// payloadToProto renders a stored source payload (the inverse of
// protoconv.ActivitySourcePayloadFromProto for the fields core keeps).
func payloadToProto(p domain.ActivitySourcePayload) *v1.ActivitySourcePayload {
	out := &v1.ActivitySourcePayload{
		Type:            activityTypeToProto(p.Type),
		Discipline:      disciplineToProto(p.Discipline),
		IsVirtual:       p.IsVirtual,
		IsEbike:         p.IsEbike,
		IsCommute:       p.IsCommute,
		IsRace:          p.IsRace,
		CustomSubtype:   p.CustomSubtype,
		Title:           p.Title,
		Description:     p.Description,
		StartTime:       timestamppb.New(p.StartTime),
		EndTime:         timestamppb.New(p.EndTime),
		ElapsedDuration: durationpb.New(p.ElapsedDuration),
		MovingDuration:  durationpb.New(p.MovingDuration),
		Timezone:        p.Timezone,
		Summary:         summaryToProto(p.Summary),
	}
	for i, l := range p.Laps {
		out.Laps = append(out.Laps, lapToProto(l, i))
	}
	if p.HasStream {
		out.StreamSummary = &v1.StreamSummary{}
	}
	return out
}
