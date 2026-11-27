package view

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/adapter/secondary/inmem"
	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

type fakeSegments struct {
	port.SegmentReader
	efforts []domain.SegmentEffort
	segs    map[domain.SegmentID]domain.Segment
	similar []domain.SimilarActivity
}

func (f *fakeSegments) ListEffortsForActivity(context.Context, domain.ActivityID) ([]domain.SegmentEffort, error) {
	return f.efforts, nil
}
func (f *fakeSegments) GetSegment(_ context.Context, id domain.SegmentID) (domain.Segment, error) {
	return f.segs[id], nil
}
func (f *fakeSegments) FindSimilarActivities(context.Context, domain.SimilarRouteQuery) ([]domain.SimilarActivity, error) {
	return f.similar, nil
}

type fakeStreams struct {
	port.StreamRepo
	samples []domain.StreamSample
}

func (f *fakeStreams) QueryStream(_ context.Context, q domain.StreamQuery) (domain.Stream, error) {
	return domain.Stream{ActivitySourceID: q.ActivitySourceID, Samples: f.samples}, nil
}

type fakePresence struct{ workers []domain.LiveWorker }

func (f fakePresence) LiveWorkers(context.Context) []domain.LiveWorker { return f.workers }

func f64p(v float64) *float64 { return &v }

func TestDetailsLapsAndEfforts(t *testing.T) {
	repo := inmem.NewActivityRepo()
	user := domain.UserID(uuid.New())
	aid := domain.ActivityID(uuid.New())
	s1, s2 := domain.SourceID(uuid.New()), domain.SourceID(uuid.New())
	repo.Activities[aid] = domain.Activity{ID: aid, UserID: user, PrimaryStreamSourceID: &s1}
	repo.Sources[s1] = domain.ActivitySource{ID: s1, ActivityID: aid, UserID: user, Status: domain.SourceStatusActive, Parsed: domain.ActivitySourcePayload{Laps: []domain.ActivityLap{{}, {}}}}
	repo.Sources[s2] = domain.ActivitySource{ID: s2, ActivityID: aid, UserID: user, Status: domain.SourceStatusActive, Parsed: domain.ActivitySourcePayload{Laps: []domain.ActivityLap{{}}}}

	start := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	samples := make([]domain.StreamSample, 100)
	for i := range samples {
		samples[i] = domain.StreamSample{Timestamp: start.Add(time.Duration(i) * time.Second), DistanceM: f64p(float64(i * 10))}
	}
	segID := domain.SegmentID(uuid.New())
	segs := &fakeSegments{
		efforts: []domain.SegmentEffort{{SegmentID: segID, ActivityID: aid, ActivitySourceID: s1, StartOffset: 10, EndOffset: 50, ElapsedS: 40, PersonalRank: 1}},
		segs:    map[domain.SegmentID]domain.Segment{segID: {ID: segID, Name: "Hill", DistanceM: 400, ClimbCategory: domain.ClimbCategoryNone}},
	}
	d := NewDetails(repo, &fakeStreams{samples: samples}, segs, nil, nil, nil)
	act := repo.Activities[aid]

	laps, sid, err := d.Laps(context.Background(), act)
	if err != nil || len(laps) != 2 || sid == nil || *sid != s1 {
		t.Fatalf("laps %d sid %v err %v", len(laps), sid, err)
	}
	rows, err := d.SegmentEfforts(context.Background(), act)
	if err != nil || len(rows) != 1 {
		t.Fatalf("efforts %v %v", rows, err)
	}
	r := rows[0]
	if r.Segment.Name != "Hill" || r.StartOffsetS == nil || *r.StartOffsetS != 10 || *r.EndOffsetS != 50 || *r.StartDistanceM != 100 || *r.EndDistanceM != 500 {
		t.Fatalf("row %+v", r)
	}
	if _, err := NewDetails(repo, nil, nil, nil, nil, nil).SegmentEfforts(context.Background(), act); err == nil {
		t.Fatal("no segments repo must be unavailable")
	}
}

func TestDetailsZonesSimilarMergedManage(t *testing.T) {
	repo := inmem.NewActivityRepo()
	user := domain.UserID(uuid.New())
	aid := domain.ActivityID(uuid.New())
	s1 := domain.SourceID(uuid.New())
	d10 := 10000.0
	repo.Activities[aid] = domain.Activity{ID: aid, UserID: user, PrimaryStreamSourceID: &s1, StartLat: f64p(49), StartLng: f64p(8),
		Summary: domain.ActivitySummary{DistanceM: &d10}, MovingDuration: 50 * time.Minute, MergeProvenance: domain.MergeProvenance{domain.FieldGroupTitle: s1}}
	repo.Sources[s1] = domain.ActivitySource{ID: s1, ActivityID: aid, UserID: user, Status: domain.SourceStatusActive, Provider: "garmin",
		SourceWorkerPackage: "garmin", SourceWorkerVersion: "1.1", RawBlobID: "b", Parsed: domain.ActivitySourcePayload{HasStream: true}}
	start := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	hr := int16(150)
	samples := make([]domain.StreamSample, 700)
	for i := range samples {
		samples[i] = domain.StreamSample{Timestamp: start.Add(time.Duration(i) * time.Second), HeartRateBpm: &hr, SpeedMps: f64p(3)}
	}
	segs := &fakeSegments{similar: []domain.SimilarActivity{{ActivityID: domain.ActivityID(uuid.New()), StartTime: start.AddDate(0, 0, -1), DistanceM: &d10, MovingS: 2700, SharedSegments: 1, TargetSegments: 1}}}
	presence := fakePresence{workers: []domain.LiveWorker{{Provider: "garmin", Package: "garmin", Version: "1.2", Compatible: true}}}
	d := NewDetails(repo, &fakeStreams{samples: samples}, segs, nil, nil, presence)
	act := repo.Activities[aid]

	// No athlete profile → HR has no reference → no zones, but no error.
	z, err := d.Zones(context.Background(), act)
	if err != nil || z.HR != nil || z.Power != nil {
		t.Fatalf("zones %+v %v", z, err)
	}
	g, err := d.Similar(context.Background(), act)
	if err != nil || len(g.Rows) != 2 || g.CurrentRank != 2 || g.TargetSegments != 1 {
		t.Fatalf("similar %+v %v", g, err)
	}
	merged, has, err := d.MergedStream(context.Background(), act)
	if err != nil || !has || len(merged.Grid) == 0 {
		t.Fatalf("merged has=%v grid=%d err=%v", has, len(merged.Grid), err)
	}
	info, err := d.Manage(context.Background(), act)
	if err != nil || len(info.Sources) != 1 || !info.Sources[0].ReparseEligible || info.Sources[0].Upgrade != "new_parser" || len(info.Provenance) != 1 {
		t.Fatalf("manage %+v %v", info, err)
	}
	src, err := d.ParsedSource(context.Background(), act, s1)
	if err != nil || src.ID != s1 {
		t.Fatalf("parsed %v %v", src, err)
	}
	if _, err := d.ParsedSource(context.Background(), act, domain.SourceID(uuid.New())); err == nil {
		t.Fatal("unknown source must fail")
	}
}

func TestSegmentElevationUsesEffortSourceAndPrivacyZones(t *testing.T) {
	repo := inmem.NewActivityRepo()
	aid := domain.ActivityID(uuid.New())
	sid := domain.SourceID(uuid.New())
	source := domain.ActivitySource{ID: sid, ActivityID: aid, Provider: "garmin", Status: domain.SourceStatusActive}
	repo.Sources[sid] = source
	now := time.Now()
	var samples []domain.StreamSample
	for i, lat := range []float64{51, 51, 51.01, 51, 51} {
		samples = append(samples, domain.StreamSample{
			Timestamp: now.Add(time.Duration(i) * time.Second), Latitude: f64p(lat), Longitude: f64p(4),
			DistanceM: f64p(float64(100 + i*10)), AltitudeM: f64p(float64(i - 5)),
		})
	}
	details := NewDetails(repo, &fakeStreams{samples: samples}, nil, nil, nil, nil)
	effort := domain.SegmentEffort{ActivityID: aid, ActivitySourceID: sid, StartOffset: 1, EndOffset: 3}
	member := Viewer{Zones: []domain.PrivacyZone{{Lat: 51.01, Lng: 4, RadiusM: 20}}}
	points := details.SegmentElevation(context.Background(), effort, member)
	if len(points) != 3 || *points[0].DistanceM != 0 || *points[0].ElevationM != -4 ||
		points[1].ElevationM != nil || *points[2].DistanceM != 20 || points[2].TimeS != 2 {
		t.Fatalf("unexpected profile: %+v", points)
	}
	source.Provider = "strava"
	repo.Sources[sid] = source
	if points := details.SegmentElevation(context.Background(), effort, member); points != nil {
		t.Fatalf("restricted source yielded %+v", points)
	}
	if points := details.SegmentElevation(context.Background(), effort, Viewer{IsOwner: true}); len(points) != 3 {
		t.Fatalf("owner profile: %+v", points)
	}
}
