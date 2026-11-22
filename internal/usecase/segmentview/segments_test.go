package segmentview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

var vNow = time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC)

type fakeSegBrowse struct {
	port.SegmentReader
	seg   domain.Segment
	owns  bool
	gotQ  domain.SegmentEffortQuery
	cands []domain.SegmentEffortCandidate // fastest first
}

func (f *fakeSegBrowse) GetSegment(context.Context, domain.SegmentID) (domain.Segment, error) {
	return f.seg, nil
}
func (f *fakeSegBrowse) ViewerOwnsAccount(context.Context, domain.UserID, domain.ExternalAccountID) (bool, error) {
	return f.owns, nil
}
func (f *fakeSegBrowse) ListSegmentEffortsFiltered(_ context.Context, _ domain.SegmentID, _ domain.UserID, q domain.SegmentEffortQuery) ([]domain.SegmentEffortCandidate, error) {
	f.gotQ = q
	var out []domain.SegmentEffortCandidate
	seen := map[domain.UserID]bool{}
	for _, c := range f.cands {
		if q.UserID != nil && *q.UserID != c.Effort.UserID {
			continue
		}
		if q.BestPerUser && (seen[c.Effort.UserID] || c.Effort.ElapsedS <= 0) {
			continue
		}
		seen[c.Effort.UserID] = true
		out = append(out, c)
	}
	if q.Offset >= len(out) {
		return nil, nil
	}
	return out[q.Offset:min(len(out), q.Offset+q.Limit)], nil
}

func cand(user domain.UserID, elapsed float64) domain.SegmentEffortCandidate {
	return domain.SegmentEffortCandidate{
		Effort: domain.SegmentEffort{ID: domain.SegmentEffortID(uuid.New()), ActivityID: domain.ActivityID(uuid.New()),
			UserID: user, ElapsedS: elapsed, StartTime: vNow.Add(time.Duration(elapsed) * time.Second)},
	}
}

func TestSegmentsEffortsAndLeaderboardScope(t *testing.T) {
	viewer, other, friend := domain.UserID(uuid.New()), domain.UserID(uuid.New()), domain.UserID(uuid.New())
	segID := domain.SegmentID(uuid.New())
	f := &fakeSegBrowse{
		seg: domain.Segment{ID: segID, Source: domain.SegmentSourceNative, Scope: domain.SegmentScopePrivate, OwnerUserID: &viewer},
		cands: []domain.SegmentEffortCandidate{
			cand(other, 0),
			cand(other, 90),
			cand(friend, 95),
			cand(other, 100),
			cand(viewer, 120),
			cand(other, 130),
			cand(friend, 140),
		},
	}
	s := NewSegments(f)

	// Private: efforts are pinned to the viewer and the board is just them.
	if got, err := s.Efforts(t.Context(), viewer, segID, domain.SegmentEffortQuery{}); err != nil || f.gotQ.UserID == nil || *f.gotQ.UserID != viewer || len(got) != 1 {
		t.Fatalf("private efforts: %v %+v %d", err, f.gotQ, len(got))
	}
	entries, mine, err := s.Leaderboard(t.Context(), viewer, segID, domain.LeaderboardQuery{})
	if err != nil || len(entries) != 1 || entries[0].Rank != 1 || entries[0].UserID != viewer || mine == nil {
		t.Fatalf("private board: %v %+v", err, entries)
	}
	// Instance: everyone's efforts the viewer may see, ranked per athlete.
	f.seg.Scope = domain.SegmentScopeInstance
	got, err := s.Efforts(t.Context(), viewer, segID, domain.SegmentEffortQuery{Limit: 5000})
	if err != nil || f.gotQ.UserID != nil || len(got) != 7 {
		t.Fatalf("instance efforts: %v %+v %d", err, f.gotQ, len(got))
	}
	entries, mine, err = s.Leaderboard(t.Context(), viewer, segID, domain.LeaderboardQuery{})
	if err != nil || len(entries) != 3 || entries[0].UserID != other || entries[0].ElapsedS != 90 || entries[1].UserID != friend || mine == nil || mine.Rank != 3 {
		t.Fatalf("instance board: %v %+v %+v", err, entries, mine)
	}
	// A member who has never ridden the segment sees the same leaderboard.
	stranger := domain.UserID(uuid.New())
	entries, mine, err = s.Leaderboard(t.Context(), stranger, segID, domain.LeaderboardQuery{})
	if err != nil || len(entries) != 3 || entries[0].UserID != other || entries[0].Rank != 1 || mine != nil {
		t.Fatalf("stranger board: %v %+v", err, entries)
	}
	entries, _, _ = s.Leaderboard(t.Context(), viewer, segID, domain.LeaderboardQuery{Limit: 1, Offset: 1})
	if len(entries) != 1 || entries[0].UserID != friend || entries[0].Rank != 2 {
		t.Fatalf("paged board: %+v", entries)
	}
	// A mirror of someone else's account is invisible.
	f.seg = domain.Segment{ID: segID, Source: domain.SegmentSourceExternal, ExternalAccountID: &[]domain.ExternalAccountID{domain.ExternalAccountID(uuid.New())}[0]}
	if _, err := s.Visible(t.Context(), viewer, segID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign mirror: %v", err)
	}
	f.owns = true
	if _, err := s.Visible(t.Context(), viewer, segID); err != nil {
		t.Fatalf("own mirror: %v", err)
	}
	if _, err := s.SearchBBox(t.Context(), viewer, port.BoundingBox{MinLat: 2, MaxLat: 1}, nil, true, 10); err == nil {
		t.Fatal("inverted bbox must fail")
	}
	_ = time.Now
}
