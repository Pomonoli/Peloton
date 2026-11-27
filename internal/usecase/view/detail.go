package view

import (
	"context"
	"fmt"
	"math"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/domain/streamalign"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Details are the per-activity detail reads of the activity page and the
// manage view, shared by REST and Connect. Optional repos may be nil; the
// corresponding read then reports ErrUnavailable.
type Details struct {
	activities port.SourceReader
	streams    port.StreamRepo
	segments   port.SegmentReader
	athlete    port.AthleteProfileRepo
	overrides  port.FieldOverrideRepo
	presence   port.WorkerPresence
}

// NewDetails wires the reads; every repo but activities may be nil.
func NewDetails(activities port.SourceReader, streams port.StreamRepo, segments port.SegmentReader, athlete port.AthleteProfileRepo,
	overrides port.FieldOverrideRepo, presence port.WorkerPresence) *Details {
	return &Details{activities: activities, streams: streams, segments: segments, athlete: athlete, overrides: overrides, presence: presence}
}

// ErrUnavailable: the read needs a subsystem this instance has not wired.
var ErrUnavailable = port.NewActionError(port.ActionErrorUnavailable, "not available on this instance")

// Laps returns the lap segmentation of the source with the most laps.
func (d *Details) Laps(ctx context.Context, act domain.Activity) ([]domain.ActivityLap, *domain.SourceID, error) {
	sources, err := d.activities.ListSourcesForActivity(ctx, act.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list sources: %w", err)
	}
	sources, _ = domain.CanonicalSources(sources)
	laps := domain.PickLaps(sources)
	for _, s := range sources {
		if len(s.Parsed.Laps) == len(laps) && len(laps) > 0 {
			id := s.ID
			return laps, &id, nil
		}
	}
	return laps, nil, nil
}

// Zones computes time-in-zone from the primary stream and the athlete's
// thresholds as of the activity's date. An activity without a stream yields
// an empty result.
func (d *Details) Zones(ctx context.Context, act domain.Activity) (domain.ZonesResult, error) {
	if d.streams == nil {
		return domain.ZonesResult{}, ErrUnavailable
	}
	if act.PrimaryStreamSourceID == nil {
		return domain.ZonesResult{}, nil
	}
	stream, err := d.streams.QueryStream(ctx, domain.StreamQuery{
		ActivitySourceID: *act.PrimaryStreamSourceID,
		Channels:         []domain.StreamChannel{domain.StreamChannelHeartRate, domain.StreamChannelPower},
		Resolution:       domain.StreamResolutionRaw,
	})
	if err != nil {
		return domain.ZonesResult{}, fmt.Errorf("stream query: %w", err)
	}
	return domain.ComputeZones(stream.Samples, d.thresholds(ctx, act)), nil
}

func (d *Details) thresholds(ctx context.Context, act domain.Activity) domain.ThresholdAt {
	var prof *domain.AthleteProfile
	if d.athlete != nil {
		if entries, err := d.athlete.ListEntries(ctx, act.UserID); err == nil {
			prof = domain.NewAthleteProfile(entries)
		}
	}
	return func(k domain.AthleteMetricKey) (float64, bool) {
		if prof == nil {
			return 0, false
		}
		return prof.ValueAt(k, act.StartTime)
	}
}

// EffortRow is a segment effort joined to its segment and located on the
// source stream (time since stream start, cumulative distance) when the raw
// stream is available.
type EffortRow struct {
	Effort                       domain.SegmentEffort
	Segment                      domain.Segment
	StartOffsetS, EndOffsetS     *float64
	StartDistanceM, EndDistanceM *float64
}

// SegmentEfforts lists the activity's efforts with their segments.
func (d *Details) SegmentEfforts(ctx context.Context, act domain.Activity) ([]EffortRow, error) {
	if d.segments == nil {
		return nil, ErrUnavailable
	}
	efforts, err := d.segments.ListEffortsForActivity(ctx, act.ID)
	if err != nil {
		return nil, fmt.Errorf("list efforts: %w", err)
	}
	segs := map[domain.SegmentID]domain.Segment{}
	samples := map[domain.SourceID][]domain.StreamSample{}
	out := make([]EffortRow, 0, len(efforts))
	for _, e := range efforts {
		seg, ok := segs[e.SegmentID]
		if !ok {
			seg, _ = d.segments.GetSegment(ctx, e.SegmentID)
			segs[e.SegmentID] = seg
		}
		s, ok := samples[e.ActivitySourceID]
		if !ok {
			s = d.rawSamples(ctx, e.ActivitySourceID)
			samples[e.ActivitySourceID] = s
		}
		out = append(out, locateEffort(e, seg, s))
	}
	return out, nil
}

func (d *Details) rawSamples(ctx context.Context, sid domain.SourceID) []domain.StreamSample {
	if d.streams == nil {
		return nil
	}
	st, err := d.streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: sid, Resolution: domain.StreamResolutionRaw})
	if err != nil {
		return nil
	}
	return st.Samples
}

// locateEffort maps the raw-stream sample offsets to time + distance.
func locateEffort(e domain.SegmentEffort, seg domain.Segment, s []domain.StreamSample) EffortRow {
	row := EffortRow{Effort: e, Segment: seg}
	if len(s) == 0 || e.StartOffset < 0 || e.StartOffset >= len(s) || e.EndOffset < 0 || e.EndOffset >= len(s) {
		return row
	}
	base := s[0].Timestamp
	start, end := s[e.StartOffset].Timestamp.Sub(base).Seconds(), s[e.EndOffset].Timestamp.Sub(base).Seconds()
	row.StartOffsetS, row.EndOffsetS = &start, &end
	row.StartDistanceM, row.EndDistanceM = s[e.StartOffset].DistanceM, s[e.EndOffset].DistanceM
	return row
}

// ProfilePoint is a sample measured from the start of the segment effort.
type ProfilePoint struct {
	TimeS      float64  `json:"time_s"`
	DistanceM  *float64 `json:"distance_m"`
	ElevationM *float64 `json:"elevation_m"`
}

// SegmentElevation reads the effort's actual source and only returns samples
// within the effort. Callers must resolve the parent activity for the viewer.
func (d *Details) SegmentElevation(ctx context.Context, effort domain.SegmentEffort, viewer Viewer) []ProfilePoint {
	if d.streams == nil || effort.StartOffset < 0 || effort.EndOffset <= effort.StartOffset {
		return nil
	}
	source, err := d.activities.GetSource(ctx, effort.ActivitySourceID)
	if err != nil || source.ActivityID != effort.ActivityID {
		return nil
	}
	if !viewer.IsOwner && (source.Status == domain.SourceStatusDetached || domain.ProviderDataRestricted(source.Provider)) {
		return nil
	}
	samples := d.rawSamples(ctx, source.ID)
	if effort.EndOffset >= len(samples) {
		return nil
	}
	samples = samples[effort.StartOffset : effort.EndOffset+1]
	first := samples[0]
	points := make([]ProfilePoint, 0, len(samples))
	valid := 0
	for _, sample := range samples {
		point := ProfilePoint{TimeS: sample.Timestamp.Sub(first.Timestamp).Seconds()}
		if finiteProfileValue(first.DistanceM) && finiteProfileValue(sample.DistanceM) {
			dist := *sample.DistanceM - *first.DistanceM
			if dist >= 0 {
				point.DistanceM = &dist
			}
		}
		if finiteProfileValue(sample.AltitudeM) && (len(viewer.Zones) == 0 ||
			(finiteProfileValue(sample.Latitude) && finiteProfileValue(sample.Longitude) &&
				!domain.PointInAnyZone(viewer.Zones, *sample.Latitude, *sample.Longitude))) {
			point.ElevationM = sample.AltitudeM
			valid++
		}
		points = append(points, point)
	}
	if valid < 2 {
		return nil
	}
	return points
}

func finiteProfileValue(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0)
}

// Similar groups the activity with directionally overlapping GPS routes.
func (d *Details) Similar(ctx context.Context, act domain.Activity) (domain.SimilarGroup, error) {
	if d.segments == nil {
		return domain.SimilarGroup{}, ErrUnavailable
	}
	var matches []domain.SimilarActivity
	if q, ok := domain.SimilarRouteQueryFor(act); ok {
		var err error
		matches, err = d.segments.FindSimilarActivities(ctx, q)
		if err != nil {
			return domain.SimilarGroup{}, fmt.Errorf("find similar: %w", err)
		}
	}
	return domain.BuildSimilarGroup(act, matches), nil
}

// MergedStream aligns the 5 s streams of every source onto one grid. ok is
// false when no source has a stream.
func (d *Details) MergedStream(ctx context.Context, act domain.Activity) (streamalign.MergedStream, bool, error) {
	if d.streams == nil {
		return streamalign.MergedStream{}, false, ErrUnavailable
	}
	sources, err := d.activities.ListSourcesForActivity(ctx, act.ID)
	if err != nil {
		return streamalign.MergedStream{}, false, fmt.Errorf("list sources: %w", err)
	}
	streams := map[domain.SourceID]domain.Stream{}
	independent := map[domain.SourceID]domain.Stream{}
	for _, s := range sources {
		if !s.Parsed.HasStream || (port.IsCommunityActivityRead(ctx) && (s.Status == domain.SourceStatusDetached || domain.ProviderDataRestricted(s.Provider))) {
			continue
		}
		st, err := d.streams.QueryStream(ctx, domain.StreamQuery{ActivitySourceID: s.ID, Resolution: domain.StreamResolution5s})
		if err != nil || len(st.Samples) == 0 {
			continue
		}
		if act.Type == domain.ActivityTypeRide {
			st.ActivitySourceID, st.Resolution = s.ID, domain.StreamResolution5s
			if err := ApplyCyclingStreamSpeed(ctx, d.streams, &st); err != nil {
				return streamalign.MergedStream{}, false, fmt.Errorf("validate stream: %w", err)
			}
		}
		streams[s.ID] = st
		if !domain.ProviderDataRestricted(s.Provider) {
			independent[s.ID] = st
		}
	}
	if len(streams) == 0 {
		return streamalign.MergedStream{}, false, nil
	}
	merged := streamalign.Build(streams, streamalign.PreferredWinners(independent, streams), streamalign.Options{})
	if act.PrimaryStreamSourceID != nil {
		if st, ok := streams[*act.PrimaryStreamSourceID]; ok {
			merged.PrimaryStart = st.Samples[0].Timestamp
		}
	}
	return merged, true, nil
}

// Manage assembles the technical manage view of the owner's activity.
func (d *Details) Manage(ctx context.Context, act domain.Activity) (domain.ManageInfo, error) {
	var sources []domain.ActivitySource
	var err error
	if port.IsCommunityActivityRead(ctx) {
		sources, err = d.activities.ListSourcesForActivity(ctx, act.ID)
		for i, src := range sources {
			sources[i] = domain.ActivitySource{
				ID: src.ID, ActivityID: src.ActivityID, Provider: src.Provider, Status: domain.SourceStatusActive,
				Parsed: domain.ActivitySourcePayload{HasStream: src.Parsed.HasStream, Laps: src.Parsed.Laps},
			}
		}
	} else {
		sources, err = d.activities.ListAllSourcesForActivity(ctx, act.ID)
	}
	if err != nil {
		return domain.ManageInfo{}, fmt.Errorf("list sources: %w", err)
	}
	manual := map[domain.FieldGroup]bool{}
	if d.overrides != nil {
		if pins, err := d.overrides.ListForActivity(ctx, act.ID); err == nil {
			for _, p := range pins {
				manual[p.FieldKey] = true
			}
		}
	}
	var workers []domain.LiveWorker
	if d.presence != nil && !port.IsCommunityActivityRead(ctx) {
		workers = d.presence.LiveWorkers(ctx)
	}
	return domain.BuildManageInfo(act, sources, workers, manual), nil
}

// ParsedSource returns one of the activity's sources (any status) as stored.
func (d *Details) ParsedSource(ctx context.Context, act domain.Activity, sourceID domain.SourceID) (domain.ActivitySource, error) {
	src, err := d.activities.GetSource(ctx, sourceID)
	if err != nil {
		return domain.ActivitySource{}, err
	}
	if src.ActivityID != act.ID {
		return domain.ActivitySource{}, domain.ErrNotFound
	}
	return src, nil
}
