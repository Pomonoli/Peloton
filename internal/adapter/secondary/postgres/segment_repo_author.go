package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// segmentVisibleSQL is the predicate for "viewer $N may see segment s".
func segmentVisibleSQL(viewerParam string) string {
	return `(
		(s.source = 'native' AND s.scope = 'instance')
		OR (s.source = 'native' AND s.owner_user_id = ` + viewerParam + `)
		OR (s.source = 'external' AND s.external_account_id IN (
			SELECT id FROM external_accounts WHERE user_id = ` + viewerParam + `))
		OR (s.source = 'strava_catalog' AND EXISTS (
			SELECT 1 FROM segment_efforts e JOIN activities a ON a.id = e.activity_id
			WHERE e.segment_id = s.id AND a.hidden_by_admin = false AND ` + catalogEffortSQL + `))
	)`
}

func (r *SegmentRepo) ViewerOwnsAccount(ctx context.Context, viewer domain.UserID, account domain.ExternalAccountID) (bool, error) {
	db := dbtx(ctx, r.pool)
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM external_accounts WHERE id = $1 AND user_id = $2)`,
		account.UUID(), viewer.UUID()).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("viewer owns account: %w", err)
	}
	return ok, nil
}

func (r *SegmentRepo) CountEffortsForSegment(ctx context.Context, segmentID domain.SegmentID) (int, error) {
	db := dbtx(ctx, r.pool)
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM segment_efforts WHERE segment_id = $1`, segmentID.UUID()).Scan(&n); err != nil {
		return 0, fmt.Errorf("count efforts: %w", err)
	}
	return n, nil
}

func (r *SegmentRepo) ListSegments(ctx context.Context, viewer domain.UserID, f domain.SegmentListFilter) ([]domain.Segment, int, error) {
	db := dbtx(ctx, r.pool)
	where := []string{segmentVisibleSQL("$1")}
	args := []any{viewer.UUID()}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if len(f.Sources) > 0 {
		add("s.source = ANY($%d)", stringsOf(f.Sources))
	}
	if len(f.Scopes) > 0 {
		add("s.scope = ANY($%d)", stringsOf(f.Scopes))
	}
	if len(f.ActivityTypes) > 0 {
		add("s.activity_type = ANY($%d)", stringsOf(f.ActivityTypes))
	}
	if f.OnlyStarred {
		where = append(where, "s.starred")
	}
	if f.OnlyWithEfforts {
		where = append(where, "EXISTS (SELECT 1 FROM segment_efforts e WHERE e.segment_id = s.id AND e.user_id = $1)")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, q)
		n := len(args)
		where = append(where, fmt.Sprintf("(s.name ILIKE '%%' || $%d || '%%' OR s.description ILIKE '%%' || $%d || '%%')", n, n))
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM segments s WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count segments: %w", err)
	}
	limit, offset := f.Limit, max(0, f.Offset)
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	rows, err := db.Query(ctx,
		`SELECT `+prefixedColumns(segmentColumns, "s.")+`
		   FROM segments s
		  WHERE `+cond+`
		  ORDER BY `+segmentOrderSQL(f.Sort)+`
		  LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list segments: %w", err)
	}
	out, err := pgCollectRows(rows, scanSegmentRow, "scan segment", "iterate segments")
	return out, total, err
}

// segmentOrderSQL maps a sort to its ORDER BY; the viewer's effort count
// sort uses the bound viewer parameter $1.
func segmentOrderSQL(sort domain.SegmentSort) string {
	switch sort {
	case domain.SegmentSortDistanceDesc:
		return "s.distance_m DESC, s.name ASC"
	case domain.SegmentSortElevationDesc:
		return "s.elevation_gain_m DESC NULLS LAST, s.name ASC"
	case domain.SegmentSortEffortCountDesc:
		return "(SELECT count(*) FROM segment_efforts e WHERE e.segment_id = s.id AND e.user_id = $1) DESC, s.name ASC"
	case domain.SegmentSortCreatedAtDesc:
		return "s.created_at DESC"
	}
	return "s.name ASC"
}

func (r *SegmentRepo) SearchSegmentsInBBox(ctx context.Context, viewer domain.UserID, box port.BoundingBox, types []domain.ActivityType, includeInstance bool, limit int) ([]domain.Segment, error) {
	db := dbtx(ctx, r.pool)
	if limit <= 0 {
		limit = 100
	}
	visible := segmentVisibleSQL("$1")
	if !includeInstance {
		visible = `((s.source = 'native' AND s.owner_user_id = $1)
			OR (s.source = 'external' AND s.external_account_id IN (SELECT id FROM external_accounts WHERE user_id = $1)))`
	}
	rows, err := db.Query(ctx,
		`SELECT `+prefixedColumns(segmentColumns, "s.")+`
		   FROM segments s
		  WHERE ST_Intersects(s.bbox, ST_GeogFromText($2))
		    AND (cardinality($3::text[]) = 0 OR s.activity_type = ANY($3))
		    AND `+visible+`
		  ORDER BY s.distance_m DESC
		  LIMIT $4`,
		viewer.UUID(), bboxToPolygonWKT(box), stringsOf(types), limit)
	if err != nil {
		return nil, fmt.Errorf("search segments: %w", err)
	}
	return pgCollectRows(rows, scanSegmentRow, "scan segment", "iterate segments")
}

func (r *SegmentRepo) ListSegmentEffortsFiltered(ctx context.Context, segmentID domain.SegmentID, viewer domain.UserID, q domain.SegmentEffortQuery) ([]domain.SegmentEffortCandidate, error) {
	db := dbtx(ctx, r.pool)
	limit, offset := q.Limit, max(0, q.Offset)
	if limit <= 0 {
		limit = 50
	}
	const fastestOrder = "e.elapsed_s, e.start_time, e.id"
	order := "e.start_time DESC, e.id"
	if q.FastestFirst || q.BestPerUser {
		order = fastestOrder
	}
	columns := prefixedColumns(segmentEffortColumns, "e.") + ", COALESCE(NULLIF(u.display_name, ''), u.username::text) AS display_name, COALESCE(u.avatar_url, '') AS avatar_url"
	from := ` FROM segment_efforts e
		   JOIN activities a ON a.id = e.activity_id
		   JOIN segments s ON s.id = e.segment_id
		   JOIN users u ON u.id = e.user_id
		  WHERE e.segment_id = $1
		    AND a.deleted_at IS NULL AND ` + catalogEffortSQL + `
		    AND (a.hidden_by_admin = false OR (s.source <> 'strava_catalog' AND e.user_id = $2))
		    AND ($3::uuid IS NULL OR e.user_id = $3)
		    AND ($4::timestamptz IS NULL OR e.start_time >= $4)
		    AND ($5::timestamptz IS NULL OR e.start_time < $5)`
	query := "SELECT " + columns + from
	if q.BestPerUser {
		query = `SELECT e.* FROM (SELECT DISTINCT ON (e.user_id) ` + columns + from +
			` AND e.elapsed_s > 0 AND e.elapsed_s < 'Infinity'::float8
			    ORDER BY e.user_id, ` + fastestOrder + `) e`
	}
	rows, err := db.Query(ctx, query+" ORDER BY "+order+" LIMIT $6 OFFSET $7",
		segmentID.UUID(), viewer.UUID(), userIDParam(q.UserID), nullableTime(q.From), nullableTime(q.To), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list segment efforts: %w", err)
	}
	return pgCollectRows(rows, scanSegmentEffortCandidate, "scan effort", "iterate efforts")
}

func scanSegmentEffortCandidate(row rowScanner) (domain.SegmentEffortCandidate, error) {
	var c domain.SegmentEffortCandidate
	e, err := scanSegmentEffortRow(multiScanner{row: row, tail: []any{&c.DisplayName, &c.AvatarURL}})
	c.Effort = e
	return c, err
}

func (r *SegmentRepo) ListCatalogSegments(ctx context.Context, member *domain.UserID, limit, offset int) ([]domain.CatalogSegment, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		WITH eligible AS (
			SELECT e.id, e.segment_id, e.user_id, e.elapsed_s, e.start_time
			FROM segment_efforts e
			JOIN activities a ON a.id = e.activity_id
			JOIN segments s ON s.id = e.segment_id
			WHERE s.source = 'strava_catalog' AND a.hidden_by_admin = false AND `+catalogEffortSQL+`
		), summary AS (
			SELECT segment_id, count(DISTINCT user_id) AS members
			FROM eligible
			GROUP BY segment_id
			HAVING $1::uuid IS NULL OR bool_or(user_id = $1)
		), page AS (
			SELECT s.id, s.name, s.distance_m, summary.members
			FROM summary JOIN segments s ON s.id = summary.segment_id
			ORDER BY s.name, s.id
			LIMIT $2 OFFSET $3
		)
		SELECT winner.id, winner.name, winner.distance_m, winner.elapsed_s, COALESCE(NULLIF(u.display_name, ''), u.username::text), winner.members
		FROM (
			SELECT DISTINCT ON (p.id) p.*, e.elapsed_s, e.user_id
			FROM page p JOIN eligible e ON e.segment_id = p.id
			ORDER BY p.id, e.elapsed_s, e.start_time, e.id
		) winner
		JOIN users u ON u.id = winner.user_id
		ORDER BY winner.name, winner.id`, userIDParam(member), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list catalog segments: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (domain.CatalogSegment, error) {
		var item domain.CatalogSegment
		var id uuid.UUID
		err := row.Scan(&id, &item.Name, &item.DistanceM, &item.BestElapsedS, &item.LocalKOMHolder, &item.MemberCount)
		item.ID = domain.SegmentID(id)
		return item, err
	}, "scan catalog segment", "iterate catalog segments")
}

// multiScanner appends extra destinations to a scan, so a row scanner for a
// column list can be reused on a query that selects more.
type multiScanner struct {
	row  rowScanner
	tail []any
}

func (m multiScanner) Scan(dest ...any) error { return m.row.Scan(append(dest, m.tail...)...) }

// stringsOf renders a slice of string-kinded values for an ANY($n) bind.
func stringsOf[T ~string](in []T) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		out = append(out, string(v))
	}
	return out
}
