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
	if q.ValidOnly || q.BestPerUser {
		from += " AND e.elapsed_s > 0 AND e.elapsed_s < 'Infinity'::float8"
	}
	query := "SELECT " + columns + ", 0, 0::float8" + from
	if q.BestPerUser {
		query = `SELECT e.* FROM (SELECT DISTINCT ON (e.user_id) ` + columns + `,
			count(*) OVER (PARTITION BY e.user_id), avg(e.elapsed_s) OVER (PARTITION BY e.user_id)` + from +
			" ORDER BY e.user_id, " + fastestOrder + ") e"
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
	e, err := scanSegmentEffortRow(multiScanner{row: row, tail: []any{&c.DisplayName, &c.AvatarURL, &c.EffortCount, &c.MeanElapsedS}})
	c.Effort = e
	return c, err
}

const catalogHolderNameSQL = "COALESCE(NULLIF(u.display_name, ''), u.username::text)"

var catalogNameEscape = strings.NewReplacer("~", "~~", "%", "~%", "_", "~_")

func (r *SegmentRepo) ListCatalogSegments(ctx context.Context, q domain.CatalogSegmentQuery) ([]domain.CatalogSegment, error) {
	args := []any{userIDParam(q.Member), q.Limit, q.Offset}
	where := ""
	if q.Name != "" {
		args = append(args, "%"+catalogNameEscape.Replace(q.Name)+"%")
		where += fmt.Sprintf(" AND s.name ILIKE $%d ESCAPE '~'", len(args))
	}
	geometry := ""
	if b := q.Bounds; b != nil {
		geometry = ", s.polyline, s.polyline_precision"
		where += catalogViewportSQL(b, &args)
	}

	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		WITH eligible AS (
			SELECT e.id, e.segment_id, e.user_id, e.elapsed_s, e.start_time
			FROM segment_efforts e
			JOIN activities a ON a.id = e.activity_id
			JOIN segments s ON s.id = e.segment_id
			WHERE s.source = 'strava_catalog' AND a.hidden_by_admin = false AND `+catalogEffortSQL+where+`
		), summary AS (
			SELECT segment_id, count(DISTINCT user_id) AS members
			FROM eligible
			GROUP BY segment_id
			HAVING $1::uuid IS NULL OR bool_or(user_id = $1)
		), winner AS (
			SELECT DISTINCT ON (e.segment_id) e.segment_id, e.elapsed_s, e.user_id
			FROM eligible e JOIN summary ON summary.segment_id = e.segment_id
			ORDER BY e.segment_id, e.elapsed_s, e.start_time, e.id
		)
		SELECT s.id, s.name, s.distance_m, winner.elapsed_s, `+catalogHolderNameSQL+`, summary.members,
		       ST_X(ST_StartPoint(s.geom::geometry)), ST_Y(ST_StartPoint(s.geom::geometry))`+geometry+`
		FROM summary
		JOIN segments s ON s.id = summary.segment_id
		JOIN winner ON winner.segment_id = s.id
		JOIN users u ON u.id = winner.user_id
		ORDER BY `+catalogSegmentOrder(q.Sort, q.Descending)+` LIMIT $2 OFFSET $3`, args...)
	if err != nil {
		return nil, fmt.Errorf("list catalog segments: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (domain.CatalogSegment, error) {
		var item domain.CatalogSegment
		var id uuid.UUID
		dest := []any{&id, &item.Name, &item.DistanceM, &item.BestElapsedS, &item.LocalKOMHolder, &item.MemberCount,
			&item.Start.Lon, &item.Start.Lat}
		if q.Bounds != nil {
			dest = append(dest, &item.Polyline, &item.PolylinePrecision)
		}
		err := row.Scan(dest...)
		item.ID = domain.SegmentID(id)
		return item, err
	}, "scan catalog segment", "iterate catalog segments")
}

// Use the existing GiST index on the route geography. Segmentize the
// viewport's edges in lon/lat before casting so long latitude edges follow
// the visible rectangle closely. Wrapped views use one box on each side.
func catalogViewportSQL(b *domain.CatalogSegmentBounds, args *[]any) string {
	intersects := func(west, east float64) string {
		i := len(*args) + 1
		*args = append(*args, west, b.MinLat, east, b.MaxLat)
		return fmt.Sprintf("ST_Intersects(s.geom, ST_Segmentize(ST_MakeEnvelope($%d, $%d, $%d, $%d, 4326), 1)::geography)",
			i, i+1, i+2, i+3)
	}
	if b.MinLon < b.MaxLon {
		return " AND " + intersects(b.MinLon, b.MaxLon)
	}
	if b.MaxLon == -180 {
		return " AND " + intersects(b.MinLon, 180)
	}
	if b.MinLon == 180 {
		return " AND " + intersects(-180, b.MaxLon)
	}
	return " AND (" + intersects(b.MinLon, 180) + " OR " + intersects(-180, b.MaxLon) + ")"
}

func catalogSegmentOrder(sort domain.CatalogSegmentSort, descending bool) string {
	const nameOrder = "lower(NULLIF(btrim(s.name), ''))"
	const validDistance = "CASE WHEN s.distance_m > 0 AND s.distance_m < 'Infinity'::float8 THEN s.distance_m END"
	column := nameOrder
	switch sort {
	case domain.CatalogSortDistance:
		column = validDistance
	case domain.CatalogSortBestTime:
		column = "winner.elapsed_s"
	case domain.CatalogSortAverageSpeed:
		// Match SegmentAverageSpeedKmh. Eligible times are always positive and finite.
		column = "(" + validDistance + ") / winner.elapsed_s * 3.6"
	case domain.CatalogSortLocalKOM:
		column = "lower(NULLIF(btrim(" + catalogHolderNameSQL + "), ''))"
	case domain.CatalogSortMembers:
		column = "summary.members"
	}
	direction := " ASC"
	if descending {
		direction = " DESC"
	}
	// Use the default name order and a unique ID to keep ties consistent across pages.
	order := column + direction + " NULLS LAST"
	if column != nameOrder {
		order += ", " + nameOrder + " ASC NULLS LAST"
	}
	return order + ", s.id"
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
