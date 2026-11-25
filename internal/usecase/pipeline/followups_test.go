package pipeline

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/activity"
	"github.com/opencairn-org/cairn-core/internal/usecase/trainingload"
)

type published struct {
	subject, msgID string
	body           []byte
	ctxErr         error
}

type busSpy struct {
	err  error
	msgs []published
}

func (b *busSpy) Publish(ctx context.Context, subject, msgID string, body []byte, _ ...port.PublishOpt) error {
	b.msgs = append(b.msgs, published{subject, msgID, body, ctx.Err()})
	return b.err
}

type trainingLoadSpy struct{ users []domain.UserID }

func (s *trainingLoadSpy) Execute(_ context.Context, in trainingload.Input) (trainingload.Result, error) {
	s.users = append(s.users, in.UserID)
	return trainingload.Result{}, nil
}

// ctxStreams records the context each aggregate refresh ran under.
type ctxStreams struct {
	port.StreamRepo
	mu   sync.Mutex
	errs []error
}

type segmentEffortsStub struct {
	port.SegmentReader
	efforts []domain.SegmentEffort
}

func (s segmentEffortsStub) ListEffortsForActivity(context.Context, domain.ActivityID) ([]domain.SegmentEffort, error) {
	return s.efforts, nil
}

type activityEventsSpy []domain.ActivityEvent

func (s *activityEventsSpy) Publish(event domain.ActivityEvent) { *s = append(*s, event) }

func TestIngestFollowUpsPublishLiveSegmentRecords(t *testing.T) {
	f := newFixture()
	aid, sources := f.seed(2)
	id := domain.SegmentID(uuid.New())
	f.p.SegmentEfforts = segmentEffortsStub{efforts: []domain.SegmentEffort{
		{ActivityID: aid, ActivitySourceID: sources[0], UserID: f.user, SegmentID: id, ElapsedS: 90, IsPersonalRecord: true},
		{ActivityID: aid, ActivitySourceID: sources[0]},
		{ActivityID: aid, ActivitySourceID: sources[1], IsPersonalRecord: true},
	}}
	var events activityEventsSpy
	f.p.Events = &events
	err := f.p.RunFollowUps(t.Context(), ActivityChanged{Kind: ChangeIngested, ActivityID: aid, SourceID: sources[0], UserID: f.user})
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v, err=%v", events, err)
	}
	event := events[0]
	if event.Type != domain.ActivityEventSegmentPR || event.UserID != f.user || event.ActivityID != aid ||
		event.SourceID != sources[0] || event.Params["segment_id"] != id.String() || event.Params["elapsed_s"] != "90" {
		t.Fatalf("unexpected live record event: %+v", event)
	}
}

func (s *ctxStreams) RefreshAggregates(ctx context.Context, _ domain.SourceID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, ctx.Err())
	return nil
}

func softDeletable(f *fixture) domain.ActivityID {
	aid, srcs := f.seed(1)
	s := f.repo.Sources[srcs[0]]
	s.Status = domain.SourceStatusDetached
	f.repo.Sources[srcs[0]] = s
	return aid
}

func TestRemergePublishesInsteadOfRunningInline(t *testing.T) {
	f := newFixture()
	bus := &busSpy{}
	load := &trainingLoadSpy{}
	f.p.TrainingLoad = load
	f.p.Bus = bus
	aid := softDeletable(f)

	if _, err := f.p.Remerge(context.Background(), aid); err != nil {
		t.Fatal(err)
	}
	if len(load.users) != 0 {
		t.Fatalf("follow-ups ran inline: %v", load.users)
	}
	if len(bus.msgs) != 1 || bus.msgs[0].subject != ActivityChangedSubject || bus.msgs[0].msgID == "" {
		t.Fatalf("published: %+v", bus.msgs)
	}
	ev, err := DecodeActivityChanged(bus.msgs[0].body)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != ChangeRemerged || ev.ActivityID != aid || ev.UserID != f.user || !ev.SoftDeleted || ev.MsgID != bus.msgs[0].msgID {
		t.Fatalf("event: %+v", ev)
	}
	// The consumer side runs what the event owes.
	if err := f.p.RunFollowUps(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(load.users) != 1 || load.users[0] != f.user {
		t.Fatalf("training load was not recomputed: %v", load.users)
	}
}

func TestPublishFailureFallsBackInline(t *testing.T) {
	f := newFixture()
	load := &trainingLoadSpy{}
	f.p.TrainingLoad = load
	f.p.Bus = &busSpy{err: errors.New("nats down")}
	aid := softDeletable(f)
	if _, err := f.p.Remerge(context.Background(), aid); err != nil {
		t.Fatal(err)
	}
	if len(load.users) != 1 || load.users[0] != f.user {
		t.Fatalf("inline fallback did not run: %v", load.users)
	}
}

func TestCancelledRequestDoesNotCancelFollowUps(t *testing.T) {
	f := newFixture()
	streams := &ctxStreams{}
	f.p.Streams = streams
	aid, srcs := f.seed(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	f.p.dispatch(ctx, ActivityChanged{Kind: ChangeBorrowed, UserID: f.user, ActivityID: aid, SourceID: srcs[0]})
	if len(streams.errs) != 1 || streams.errs[0] != nil {
		t.Fatalf("inline follow-up context: %v", streams.errs)
	}

	bus := &busSpy{}
	f.p.Bus = bus
	f.p.dispatch(ctx, ActivityChanged{Kind: ChangeBorrowed, UserID: f.user, ActivityID: aid, SourceID: srcs[0]})
	if len(bus.msgs) != 1 || bus.msgs[0].ctxErr != nil {
		t.Fatalf("publish context: %+v", bus.msgs)
	}
}

func TestFollowUpsForVanishedActivityAck(t *testing.T) {
	f := newFixture()
	err := f.p.RunFollowUps(context.Background(), ActivityChanged{Kind: ChangeIngested, UserID: f.user, ActivityID: domain.ActivityID(uuid.New()), SourceID: domain.SourceID(uuid.New())})
	if err != nil {
		t.Fatal(err)
	}
}

func TestActivityChangedRoundTrip(t *testing.T) {
	in := ActivityChanged{
		Kind: ChangeIngested, UserID: domain.UserID(uuid.New()), ActivityID: domain.ActivityID(uuid.New()),
		SourceID: domain.SourceID(uuid.New()), Action: activity.IngestActionMergedIntoExisting, MsgID: "m-1",
	}
	b, err := in.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeActivityChanged(b)
	if err != nil || out != in {
		t.Fatalf("round trip: %v %+v", err, out)
	}
	for _, bad := range []string{`{`, `{"kind":"nope"}`, `{"kind":"ingested","user_id":"x"}`} {
		if _, err := DecodeActivityChanged([]byte(bad)); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
}
