// Package segmentview holds the community segment reads: browse,
// efforts and leaderboards.
package segmentview

import (
	"context"
	"fmt"
	"strings"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// ErrUnavailable: the read needs a subsystem this instance has not wired.
var ErrUnavailable = port.NewActionError(port.ActionErrorUnavailable, "not available on this instance")

// Segments are the caller-centric segment reads (SegmentService /
// GET /api/segments, /api/segments/{id}).
type Segments struct {
	segments port.SegmentReader
}

// NewSegments wires the read repository (nil reports ErrUnavailable).
func NewSegments(segments port.SegmentReader) *Segments {
	return &Segments{segments: segments}
}

// Segment list paging bounds.
const (
	SegmentsDefaultLimit = 50
	SegmentsMaxLimit     = 200
	segmentEffortsMax    = 1000
)

// MySegmentsPage is one page of the caller's segments.
type MySegmentsPage struct {
	// Stats is set on the first page only.
	Stats   *domain.UserSegmentStats
	Items   []domain.UserSegmentListItem
	HasMore bool
}

// Mine pages the caller's segments, most attempted first.
func (s *Segments) Mine(ctx context.Context, userID domain.UserID, activityType string, limit, offset int) (MySegmentsPage, error) {
	if s.segments == nil {
		return MySegmentsPage{}, ErrUnavailable
	}
	if limit <= 0 {
		limit = SegmentsDefaultLimit
	}
	if limit > SegmentsMaxLimit {
		limit = SegmentsMaxLimit
	}
	if offset < 0 {
		offset = 0
	}
	var page MySegmentsPage
	if offset == 0 {
		st, err := s.segments.UserSegmentStats(ctx, userID)
		if err != nil {
			return page, fmt.Errorf("segment stats: %w", err)
		}
		page.Stats = &st
	}
	// One extra row tells has_more without a count query.
	items, err := s.segments.ListUserSegmentsByEffortCount(ctx, userID, activityType, limit+1, offset)
	if err != nil {
		return page, fmt.Errorf("list segments: %w", err)
	}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
	}
	page.Items = items
	return page, nil
}

// Catalog pages cycling segments with community efforts by member, name, and map viewport.
func (s *Segments) Catalog(ctx context.Context, q domain.CatalogSegmentQuery) ([]domain.CatalogSegment, bool, error) {
	if s.segments == nil {
		return nil, false, ErrUnavailable
	}
	q.Name = strings.TrimSpace(q.Name)
	if q.Bounds != nil && !q.Bounds.Valid() {
		return nil, false, port.NewActionError(port.ActionErrorInvalid, "invalid segment map bounds")
	}
	seen := make(map[domain.CatalogSegmentSort]bool)
	for _, order := range q.Order {
		if !order.Column.Valid() || seen[order.Column] {
			return nil, false, port.NewActionError(port.ActionErrorInvalid, "invalid or duplicate segment sort column")
		}
		seen[order.Column] = true
	}

	limit := clampLimit(q.Limit)
	q.Limit, q.Offset = limit+1, max(0, q.Offset)
	items, err := s.segments.ListCatalogSegments(ctx, q)
	return items[:min(len(items), limit)], len(items) > limit, err
}

// Detail returns a community segment with the selected member's efforts, fastest first.
func (s *Segments) Detail(ctx context.Context, userID domain.UserID, id domain.SegmentID) (domain.Segment, []domain.SegmentEffort, error) {
	seg, err := s.Visible(ctx, userID, id)
	if err != nil {
		return domain.Segment{}, nil, err
	}
	efforts, err := s.segments.ListEffortsForSegment(ctx, id, userID, port.LeaderboardScopePersonalOnly, segmentEffortsMax, 0)
	if err != nil {
		return domain.Segment{}, nil, fmt.Errorf("list efforts: %w", err)
	}
	setCatalogPersonalRanks(seg, efforts, 0)
	return seg, efforts, nil
}

// MyEfforts pages the viewer's efforts on a visible segment, fastest first.
func (s *Segments) MyEfforts(ctx context.Context, userID domain.UserID, id domain.SegmentID, limit, offset int) ([]domain.SegmentEffort, error) {
	seg, err := s.Visible(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = SegmentsDefaultLimit
	}
	if limit > SegmentsMaxLimit {
		limit = SegmentsMaxLimit
	}
	efforts, err := s.segments.ListEffortsForSegment(ctx, id, userID, port.LeaderboardScopePersonalOnly, limit, max(0, offset))
	if err != nil {
		return nil, fmt.Errorf("list efforts: %w", err)
	}
	setCatalogPersonalRanks(seg, efforts, max(0, offset))
	return efforts, nil
}

func setCatalogPersonalRanks(seg domain.Segment, efforts []domain.SegmentEffort, offset int) {
	if seg.Source != domain.SegmentSourceStravaCatalog {
		return
	}
	for i := range efforts {
		efforts[i].PersonalRank = offset + i + 1
		efforts[i].IsPersonalRecord = efforts[i].PersonalRank == 1
	}
}

// Visible loads a community segment. Catalog entries require a valid local effort.
func (s *Segments) Visible(ctx context.Context, viewer domain.UserID, id domain.SegmentID) (domain.Segment, error) {
	if viewer == (domain.UserID{}) {
		return domain.Segment{}, domain.ErrNotFound
	}
	if s.segments == nil {
		return domain.Segment{}, ErrUnavailable
	}
	seg, err := s.segments.GetSegment(ctx, id)
	if err != nil {
		return domain.Segment{}, domain.ErrNotFound
	}
	if seg.Source == domain.SegmentSourceStravaCatalog {
		efforts, err := s.segments.ListSegmentEffortsFiltered(ctx, id, viewer, domain.SegmentEffortQuery{Limit: 1})
		if err != nil {
			return domain.Segment{}, err
		}
		if len(efforts) == 0 {
			return domain.Segment{}, domain.ErrNotFound
		}
	}
	return seg, nil
}

// List pages the segments visible to the viewer with the filter applied.
func (s *Segments) List(ctx context.Context, viewer domain.UserID, f domain.SegmentListFilter) ([]domain.Segment, int, error) {
	if s.segments == nil {
		return nil, 0, ErrUnavailable
	}
	f.Limit = clampLimit(f.Limit)
	segs, total, err := s.segments.ListSegments(ctx, viewer, f)
	if err != nil {
		return nil, 0, fmt.Errorf("list segments: %w", err)
	}
	return segs, total, nil
}

// SearchBBox returns visible segments intersecting the box.
func (s *Segments) SearchBBox(ctx context.Context, viewer domain.UserID, box port.BoundingBox, types []domain.ActivityType, includeInstance bool, limit int) ([]domain.Segment, error) {
	if s.segments == nil {
		return nil, ErrUnavailable
	}
	if box.MinLat > box.MaxLat || box.MinLon > box.MaxLon {
		return nil, port.NewActionError(port.ActionErrorInvalid, "bounding box min must not exceed max")
	}
	segs, err := s.segments.SearchSegmentsInBBox(ctx, viewer, box, types, includeInstance, clampLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("search segments: %w", err)
	}
	return segs, nil
}

// Efforts lists community efforts for the selected segment.
func (s *Segments) Efforts(ctx context.Context, viewer domain.UserID, id domain.SegmentID, q domain.SegmentEffortQuery) ([]domain.SegmentEffort, error) {
	if _, err := s.Visible(ctx, viewer, id); err != nil {
		return nil, err
	}
	q.Limit, q.Offset = clampLimit(q.Limit), max(0, q.Offset)
	rows, err := s.segments.ListSegmentEffortsFiltered(ctx, id, viewer, q)
	if err != nil {
		return nil, fmt.Errorf("list efforts: %w", err)
	}
	out := make([]domain.SegmentEffort, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Effort)
	}
	return out, nil
}

// Leaderboard ranks each member's fastest valid effort. Exact ties are ordered
// by the time of the effort and then its ID, matching the catalog's Local KOM.
func (s *Segments) Leaderboard(ctx context.Context, viewer domain.UserID, id domain.SegmentID, q domain.LeaderboardQuery) ([]domain.LeaderboardEntry, *domain.LeaderboardEntry, error) {
	seg, err := s.Visible(ctx, viewer, id)
	if err != nil {
		return nil, nil, err
	}
	const scanPage = 200
	eq := domain.SegmentEffortQuery{From: q.From, To: q.To, BestPerUser: true, Limit: scanPage}
	limit, offset := clampLimit(q.Limit), max(0, q.Offset)
	ranked := make([]domain.LeaderboardEntry, 0, limit)
	var mine *domain.LeaderboardEntry
	for {
		rows, err := s.segments.ListSegmentEffortsFiltered(ctx, id, viewer, eq)
		if err != nil {
			return nil, nil, fmt.Errorf("leaderboard: %w", err)
		}
		for i, row := range rows {
			pos := eq.Offset + i
			entry := domain.LeaderboardEntry{
				Rank: pos + 1, UserID: row.Effort.UserID, DisplayName: row.DisplayName, AvatarURL: row.AvatarURL,
				ElapsedS: row.Effort.ElapsedS, DistanceM: seg.DistanceM, StartTime: row.Effort.StartTime,
				EffortCount: row.EffortCount, MeanElapsedS: row.MeanElapsedS,
				EffortID: row.Effort.ID, ActivityID: row.Effort.ActivityID,
			}
			if pos >= offset && len(ranked) < limit {
				ranked = append(ranked, entry)
			}
			if entry.UserID == viewer {
				mine = &entry
			}
			if len(ranked) == limit && mine != nil {
				return ranked, mine, nil
			}
		}
		if len(rows) < scanPage {
			return ranked, mine, nil
		}
		eq.Offset += scanPage
	}
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return SegmentsDefaultLimit
	}
	return min(limit, SegmentsMaxLimit)
}
