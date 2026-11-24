package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// SegmentRepo implements port.SegmentRepo on top of PostGIS.
//
// geom and bbox are NOT NULL geography columns on the segments table. The
// adapter is responsible for reconstructing them from the encoded
// polyline at write time, since the migration deliberately doesn't carry
// the reconstruction logic in a trigger — keeping the SQL transformation
// next to the Go that produces it makes the data flow easier to follow.
type SegmentRepo struct {
	pool       *pgxpool.Pool
	routeCache similarRouteCache
}

func NewSegmentRepo(pool *pgxpool.Pool) *SegmentRepo {
	return &SegmentRepo{pool: pool}
}

// ---------------------------------------------------------------------------
// Segment CRUD
// ---------------------------------------------------------------------------

const segmentColumns = `
	id, source, external_id, external_account_id, owner_user_id,
	scope, name, description, activity_type,
	polyline, polyline_precision,
	distance_m, elevation_gain_m, elevation_loss_m,
	min_elevation_m, max_elevation_m, avg_grade, max_grade,
	climb_category,
	match_corridor_m, match_start_tolerance_m, match_end_tolerance_m,
	bidirectional, starred,
	created_at, updated_at, catalog_provider
`

const (
	// Bbox derivation in PostGIS: ST_Envelope works on geometry, so we
	// cast the LINESTRING geography to geometry, envelope it, and cast
	// the polygon back to geography(POLYGON, 4326). Robust regardless
	// of how the line spans the antimeridian (PostGIS will produce a
	// sensible result; instance operators with pathological cases can
	// override match_corridor_m to widen tolerance).
	segmentSaveGeomExpr = `ST_GeogFromText($25)`
	segmentSaveBBoxExpr = `ST_Envelope(ST_Expand(ST_GeogFromText($25)::geometry, 0.000001))::geography`
)

// segmentSaveSQL upserts one segment; geom/bbox are derived from the WKT in $25.
const segmentSaveSQL = `
		INSERT INTO segments (
			id, source, external_id, external_account_id, owner_user_id,
			scope, name, description, activity_type,
			polyline, polyline_precision,
			geom, bbox,
			distance_m, elevation_gain_m, elevation_loss_m,
			min_elevation_m, max_elevation_m, avg_grade, max_grade,
			climb_category,
			match_corridor_m, match_start_tolerance_m, match_end_tolerance_m,
			bidirectional, starred,
			created_at, updated_at, catalog_provider
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11,
			` + segmentSaveGeomExpr + `, ` + segmentSaveBBoxExpr + `,
			$12, $13, $14,
			$15, $16, $17, $18,
			$19,
			$20, $21, $22,
			$23, $24,
			COALESCE($26, now()), COALESCE($27, now()), $28
		)
		ON CONFLICT (id) DO UPDATE SET
			source = EXCLUDED.source,
			external_id = EXCLUDED.external_id,
			external_account_id = EXCLUDED.external_account_id,
			owner_user_id = EXCLUDED.owner_user_id,
			scope = EXCLUDED.scope,
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			activity_type = EXCLUDED.activity_type,
			polyline = EXCLUDED.polyline,
			polyline_precision = EXCLUDED.polyline_precision,
			geom = EXCLUDED.geom,
			bbox = EXCLUDED.bbox,
			distance_m = EXCLUDED.distance_m,
			elevation_gain_m = EXCLUDED.elevation_gain_m,
			elevation_loss_m = EXCLUDED.elevation_loss_m,
			min_elevation_m = EXCLUDED.min_elevation_m,
			max_elevation_m = EXCLUDED.max_elevation_m,
			avg_grade = EXCLUDED.avg_grade,
			max_grade = EXCLUDED.max_grade,
			climb_category = EXCLUDED.climb_category,
			match_corridor_m = EXCLUDED.match_corridor_m,
			match_start_tolerance_m = EXCLUDED.match_start_tolerance_m,
			match_end_tolerance_m = EXCLUDED.match_end_tolerance_m,
			bidirectional = EXCLUDED.bidirectional,
			starred = EXCLUDED.starred,
			catalog_provider = EXCLUDED.catalog_provider
		`

// segmentSaveArgs builds the segmentSaveSQL bind parameters.
func segmentSaveArgs(s domain.Segment, lineWKT string) []any {
	return []any{
		s.ID.UUID(), string(s.Source), strPtrParam(s.ExternalID), externalAccountIDParam(s.ExternalAccountID), userIDParam(s.OwnerUserID),
		string(s.Scope), s.Name, s.Description, string(s.ActivityType),
		s.Polyline, s.PolylinePrecision,
		s.DistanceM, s.ElevationGainM, s.ElevationLossM,
		s.MinElevationM, s.MaxElevationM, s.AvgGrade, s.MaxGrade,
		climbCategoryParam(s.ClimbCategory),
		s.MatchCorridorM, s.MatchStartToleranceM, s.MatchEndToleranceM,
		s.Bidirectional, s.Starred,
		lineWKT,
		nullableTime(s.CreatedAt), nullableTime(s.UpdatedAt), s.CatalogProvider,
	}
}

// SaveSegment upserts a segment by ID. Geometry is rebuilt from the
// encoded polyline; the LINESTRING is constructed via ST_MakeLine over
// the decoded points, and the bbox is computed via ST_Envelope of that
// line.
//
// Decoding happens in Go (the domain package owns the polyline format),
// then the resulting points are sent as a single text-encoded WKT
// LINESTRING. PostGIS does the geography projection.
func (r *SegmentRepo) SaveSegment(ctx context.Context, s domain.Segment) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("validate segment: %w", err)
	}

	points, err := s.DecodedPolyline()
	if err != nil {
		return fmt.Errorf("decode polyline for %s: %w", s.ID, err)
	}
	if len(points) < 2 {
		return fmt.Errorf("save segment %s: polyline has fewer than 2 points", s.ID)
	}

	lineWKT := pointsToLineStringWKT(points)
	db := dbtx(ctx, r.pool)
	_, err = db.Exec(ctx, segmentSaveSQL, segmentSaveArgs(s, lineWKT)...)
	if err != nil {
		return fmt.Errorf("save segment %s: %w", s.ID, err)
	}
	return nil
}

// GetSegment loads one segment by ID. Geometry columns are not returned —
// the polyline is the canonical client representation.
func (r *SegmentRepo) GetSegment(ctx context.Context, id domain.SegmentID) (domain.Segment, error) {
	db := dbtx(ctx, r.pool)
	row := db.QueryRow(ctx,
		`SELECT `+segmentColumns+` FROM segments WHERE id = $1`,
		id.UUID(),
	)
	seg, err := scanSegmentRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Segment{}, domain.ErrNotFound
		}
		return domain.Segment{}, fmt.Errorf("get segment %s: %w", id, err)
	}
	return seg, nil
}

// FindSegmentIDByExternal resolves an external (provider-mirrored) segment by
// its (account, external_id). Used by the ingest path to dedup a provider
// segment across the many activities that traverse it. Returns
// domain.ErrNotFound when no such segment exists yet.
func (r *SegmentRepo) FindSegmentIDByExternal(
	ctx context.Context,
	accountID domain.ExternalAccountID,
	externalID string,
) (domain.SegmentID, error) {
	db := dbtx(ctx, r.pool)
	var id uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT id FROM segments
		  WHERE source = 'external' AND external_account_id = $1 AND external_id = $2`,
		accountID.UUID(), externalID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SegmentID{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.SegmentID{}, fmt.Errorf("find segment by external %s: %w", externalID, err)
	}
	return domain.SegmentID(id), nil
}

// DeleteSegment hard-deletes (ON DELETE CASCADE wipes its efforts).
func (r *SegmentRepo) DeleteSegment(ctx context.Context, id domain.SegmentID) error {
	db := dbtx(ctx, r.pool)
	_, err := db.Exec(ctx, `DELETE FROM segments WHERE id = $1`, id.UUID())
	if err != nil {
		return fmt.Errorf("delete segment %s: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Spatial candidate filter
// ---------------------------------------------------------------------------

// ListSegmentCandidatesForActivity runs the PostGIS bbox-intersects query
// with visibility predicates baked in. The corridor-walking match step
// happens in Go (domain.MatchSegment); this method only narrows the set
// from "every segment in the DB" to "every segment whose bbox could plausibly
// overlap this activity AND the user is allowed to see".
func (r *SegmentRepo) ListSegmentCandidatesForActivity(
	ctx context.Context,
	userID domain.UserID,
	activityType domain.ActivityType,
	box port.BoundingBox,
) ([]domain.Segment, error) {
	db := dbtx(ctx, r.pool)

	bboxWKT := bboxToPolygonWKT(box)

	rows, err := db.Query(ctx,
		`SELECT `+segmentColumns+`
		   FROM segments
		  WHERE activity_type = $1
		    AND ST_Intersects(bbox, ST_GeogFromText($2))
		    AND (
		        -- External-mirror: only segments owned by one of this
		        -- user's external accounts. Provider identity is implicit
		        -- via external_accounts.provider.
		        (source = 'external' AND external_account_id IN (
		            SELECT id FROM external_accounts WHERE user_id = $3
		        ))
		        OR
		        -- Native private: only segments owned by this user.
		        (source = 'native' AND scope = 'private' AND owner_user_id = $3)
		        OR
		        -- Native instance: all instance-scope segments visible.
		        (source IN ('native', 'strava_catalog') AND scope = 'instance')
		    )
		  ORDER BY distance_m DESC`,
		string(activityType), bboxWKT, userID.UUID(),
	)
	if err != nil {
		return nil, fmt.Errorf("list segment candidates: %w", err)
	}
	return pgCollectRows(rows, scanSegmentRow, "scan candidate segment", "iterate segment candidates")
}

// ---------------------------------------------------------------------------
// SegmentEffort
// ---------------------------------------------------------------------------

const segmentEffortColumns = `
	id, segment_id, activity_id, activity_source_id, user_id,
	start_time, elapsed_s, moving_s,
	start_offset, end_offset,
	avg_heart_rate_bpm, max_heart_rate_bpm, avg_power_w, avg_cadence, avg_speed_mps,
	personal_rank, instance_rank, is_personal_record, is_instance_record,
	provider_effort_external_id,
	created_at
`

// SaveSegmentEffort upserts by natural key (segment_id, activity_source_id,
// start_offset). Reimports of the same source replace any previously
// recorded effort with the same key — the ranks columns are intentionally
// reset since they'll be recomputed by ComputeSegmentRanks (a separate
// follow-up use case, v2).
func (r *SegmentRepo) SaveSegmentEffort(ctx context.Context, e domain.SegmentEffort) error {
	db := dbtx(ctx, r.pool)
	_, err := db.Exec(ctx, `
		INSERT INTO segment_efforts (
			id, segment_id, activity_id, activity_source_id, user_id,
			start_time, elapsed_s, moving_s,
			start_offset, end_offset,
			avg_heart_rate_bpm, max_heart_rate_bpm, avg_power_w, avg_cadence, avg_speed_mps,
			personal_rank, instance_rank, is_personal_record, is_instance_record,
			provider_effort_external_id,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10,
			$11, $12, $13, $14, $15,
			$16, $17, $18, $19,
			$20,
			COALESCE($21, now())
		)
		ON CONFLICT (segment_id, activity_source_id, start_offset) DO UPDATE SET
			activity_id = EXCLUDED.activity_id,
			user_id = EXCLUDED.user_id,
			start_time = EXCLUDED.start_time,
			elapsed_s = EXCLUDED.elapsed_s,
			moving_s = EXCLUDED.moving_s,
			end_offset = EXCLUDED.end_offset,
			avg_heart_rate_bpm = EXCLUDED.avg_heart_rate_bpm,
			max_heart_rate_bpm = EXCLUDED.max_heart_rate_bpm,
			avg_power_w = EXCLUDED.avg_power_w,
			avg_cadence = EXCLUDED.avg_cadence,
			avg_speed_mps = EXCLUDED.avg_speed_mps,
			-- Ranks are recomputed asynchronously; reset on upsert.
			personal_rank = 0,
			instance_rank = 0,
			is_personal_record = false,
			is_instance_record = false,
			provider_effort_external_id = EXCLUDED.provider_effort_external_id
		`,
		e.ID.UUID(), e.SegmentID.UUID(), e.ActivityID.UUID(), e.ActivitySourceID.UUID(), e.UserID.UUID(),
		e.StartTime, e.ElapsedS, e.MovingS,
		e.StartOffset, e.EndOffset,
		e.AvgHeartRateBpm, e.MaxHeartRateBpm, e.AvgPowerW, e.AvgCadence, e.AvgSpeedMps,
		e.PersonalRank, e.InstanceRank, e.IsPersonalRecord, e.IsInstanceRecord,
		strPtrParam(e.ProviderEffortExternalID),
		nullableTime(e.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("save segment effort %s: %w", e.ID, err)
	}
	return nil
}

// AttachProviderEffortRef stamps the provider's effort id onto the matcher
// effort for (segment, source) that starts within toleranceS seconds of the
// provider-reported start. Matcher and provider disagree by a few samples on
// where a traversal begins; start-time proximity is the overlap signal. Picks
// the closest effort when several qualify (hill repeats).
func (r *SegmentRepo) AttachProviderEffortRef(
	ctx context.Context,
	segmentID domain.SegmentID,
	sourceID domain.SourceID,
	providerEffortExternalID string,
	startTime time.Time,
	toleranceS int,
) (bool, error) {
	db := dbtx(ctx, r.pool)
	tag, err := db.Exec(ctx, `
		UPDATE segment_efforts SET provider_effort_external_id = $3
		 WHERE id = (
		     SELECT id FROM segment_efforts
		      WHERE segment_id = $1 AND activity_source_id = $2
		        AND abs(extract(epoch FROM (start_time - $4::timestamptz))) <= $5
		      ORDER BY abs(extract(epoch FROM (start_time - $4::timestamptz)))
		      LIMIT 1
		 )`,
		segmentID.UUID(), sourceID.UUID(), providerEffortExternalID, startTime.UTC(), toleranceS,
	)
	if err != nil {
		return false, fmt.Errorf("attach provider effort ref: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// DeleteEffortsForSource wipes every effort recorded against the given
// activity source. Called by the matching use case before re-running the
// pass on a reimported source — keeps the table from accumulating stale
// rows whose stream-offset pointers may no longer be valid.
func (r *SegmentRepo) DeleteEffortsForSource(ctx context.Context, sourceID domain.SourceID) error {
	db := dbtx(ctx, r.pool)
	_, err := db.Exec(ctx,
		`DELETE FROM segment_efforts WHERE activity_source_id = $1`,
		sourceID.UUID(),
	)
	if err != nil {
		return fmt.Errorf("delete segment_efforts for %s: %w", sourceID, err)
	}
	return nil
}

func (r *SegmentRepo) ListEffortSegmentsForSource(ctx context.Context, sourceID domain.SourceID) ([]domain.SegmentID, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, `SELECT DISTINCT segment_id FROM segment_efforts WHERE activity_source_id = $1`,
		sourceID.UUID())
	if err != nil {
		return nil, fmt.Errorf("list segments for source %s: %w", sourceID, err)
	}
	return pgCollectRows(rows, func(row rowScanner) (domain.SegmentID, error) {
		var id uuid.UUID
		err := row.Scan(&id)
		return domain.SegmentID(id), err
	}, "scan effort segment", "iterate effort segments")
}

// catalogEffortSQL selects a single recording per segment and ride,
// preferring the canonical stream and retaining repeat traversals on it.
const catalogEffortSQL = `
	a.deleted_at IS NULL
	AND ` + canonicalSegmentSourceSQL + `
	AND ` + catalogLocalEffortSQL + `
	AND NOT EXISTS (
		SELECT 1 FROM segment_efforts alt
		JOIN activity_sources src ON src.id = alt.activity_source_id AND src.activity_id = a.id
		WHERE alt.segment_id = e.segment_id AND alt.activity_id = e.activity_id
		  AND alt.activity_source_id <> e.activity_source_id
		  AND ` + canonicalPerformanceSourceSQL + `
		  AND (s.source <> 'strava_catalog' OR (
		       alt.provider_effort_external_id IS NULL AND alt.elapsed_s > 0 AND alt.elapsed_s < 'Infinity'::float8))
		  AND (alt.activity_source_id = a.primary_stream_source_id OR (
		       e.activity_source_id IS DISTINCT FROM a.primary_stream_source_id
		       AND alt.activity_source_id < e.activity_source_id))
	)`

const catalogLocalEffortSQL = `(s.source <> 'strava_catalog' OR (
		e.provider_effort_external_id IS NULL AND a.type = 'ride'
		AND e.elapsed_s > 0 AND e.elapsed_s < 'Infinity'::float8
	))`

const canonicalSegmentSourceSQL = `EXISTS (
	SELECT 1 FROM activity_sources src
	WHERE src.id = e.activity_source_id AND src.activity_id = a.id
	  AND ` + canonicalPerformanceSourceSQL + `
)`

// ListEffortsForActivity returns every effort across every source of the
// activity. Ordered by start_time so the UI can render them along the
// activity's timeline.
func (r *SegmentRepo) ListEffortsForActivity(ctx context.Context, id domain.ActivityID) ([]domain.SegmentEffort, error) {
	db := dbtx(ctx, r.pool)
	rows, err := db.Query(ctx,
		`SELECT `+prefixedColumns(segmentEffortColumns, "e.")+`
		   FROM segment_efforts e
		   JOIN segments s ON s.id = e.segment_id
		   JOIN activities a ON a.id = e.activity_id
		  WHERE e.activity_id = $1 AND `+catalogEffortSQL+`
		  ORDER BY e.start_time, e.id`,
		id.UUID(),
	)
	if err != nil {
		return nil, fmt.Errorf("list efforts for %s: %w", id, err)
	}
	return pgCollectRows(rows, scanSegmentEffortRow, "scan segment effort", "iterate segment efforts")
}

// ListEffortsForSegment is the leaderboard read. Scope decides which rows.
// Adapters enforce visibility — callers shouldn't second-guess.
func (r *SegmentRepo) ListEffortsForSegment(
	ctx context.Context,
	segmentID domain.SegmentID,
	viewerUserID domain.UserID,
	scope port.LeaderboardScope,
	limit int,
	offset int,
) ([]domain.SegmentEffort, error) {
	if limit <= 0 {
		limit = 50
	}

	db := dbtx(ctx, r.pool)

	var (
		rows pgx.Rows
		err  error
	)
	switch scope {
	case port.LeaderboardScopePersonalOnly:
		rows, err = db.Query(ctx,
			`SELECT `+prefixedColumns(segmentEffortColumns, "e.")+`
			   FROM segment_efforts e
			   JOIN segments s ON s.id = e.segment_id
			   JOIN activities a ON a.id = e.activity_id
			  WHERE e.segment_id = $1 AND e.user_id = $2 AND a.deleted_at IS NULL AND `+catalogEffortSQL+communityReadSQL(ctx, "a.")+communitySegmentSQL(ctx, "a.")+`
			  ORDER BY e.elapsed_s, e.start_time, e.id
			  LIMIT $3 OFFSET $4`,
			segmentID.UUID(), viewerUserID.UUID(), limit, offset,
		)
	case port.LeaderboardScopeInstance:
		// Visibility check: the viewer must be allowed to see this segment.
		// We fold the check into the same query — if the segment is
		// instance-scope (Cairn) OR private but owned by the viewer, OR
		// Strava but owned by one of the viewer's external accounts.
		rows, err = db.Query(ctx,
			`SELECT `+prefixedColumns(segmentEffortColumns, "e.")+`
			   FROM segment_efforts e
			   JOIN segments s ON s.id = e.segment_id
			   JOIN activities a ON a.id = e.activity_id
			  WHERE e.segment_id = $1 AND NOT a.provider_restricted AND `+catalogEffortSQL+`
			    AND (
			        (s.source = 'native' AND s.scope = 'instance')
			        OR (s.source = 'native' AND s.scope = 'private' AND s.owner_user_id = $2)
			        OR (s.source = 'external' AND s.external_account_id IN (
			            SELECT id FROM external_accounts WHERE user_id = $2
			        ))
			    )
			  ORDER BY e.elapsed_s ASC
			  LIMIT $3 OFFSET $4`,
			segmentID.UUID(), viewerUserID.UUID(), limit, offset,
		)
	default:
		return nil, fmt.Errorf("unknown leaderboard scope: %q", scope)
	}
	if err != nil {
		return nil, fmt.Errorf("list efforts for segment %s: %w", segmentID, err)
	}
	return pgCollectRows(rows, scanSegmentEffortRow, "scan leaderboard row", "iterate leaderboard")
}

// ---------------------------------------------------------------------------
// Rank denormalization
// ---------------------------------------------------------------------------

// RecomputeRanksForSegment refreshes personal_rank, instance_rank,
// is_personal_record, is_instance_record for every effort on the segment.
//
// The whole pass runs in a single SQL statement: a CTE applies two
// ROW_NUMBER() window functions (one partitioned by user_id for the
// personal leaderboard, one unpartitioned for the instance leaderboard),
// then UPDATEs every row to its computed rank. PostgreSQL holds an
// internal row lock for the duration — for typical segments (10s-1000s
// of efforts) this is sub-millisecond and races between concurrent
// match passes converge on the same correct state.
//
// Note: instance_rank is computed globally across every effort regardless
// of segment scope (private/instance) or source (Strava/Cairn). The
// visibility filter for "who can SEE this leaderboard" happens at read
// time in ListEffortsForSegment — the denormalized column itself is
// just a sort key.
func (r *SegmentRepo) RecomputeRanksForSegment(ctx context.Context, segmentID domain.SegmentID) error {
	db := dbtx(ctx, r.pool)
	_, err := db.Exec(ctx, `
		WITH ranked AS (
			SELECT
				e.id,
				ROW_NUMBER() OVER (PARTITION BY e.user_id ORDER BY e.elapsed_s, e.start_time, e.id) AS personal_rank,
				CASE WHEN s.source = 'strava_catalog' OR a.provider_restricted THEN 0
				     ELSE ROW_NUMBER() OVER (PARTITION BY a.provider_restricted ORDER BY e.elapsed_s, e.start_time, e.id) END AS instance_rank
			  FROM segment_efforts e
			  JOIN segments s ON s.id = e.segment_id
			  JOIN activities a ON a.id = e.activity_id
			 WHERE e.segment_id = $1 AND `+catalogEffortSQL+`
		)
		UPDATE segment_efforts e SET
			personal_rank       = r.personal_rank,
			is_personal_record  = (r.personal_rank = 1),
			instance_rank       = r.instance_rank,
			is_instance_record  = (r.instance_rank = 1)
		FROM ranked r
		WHERE e.id = r.id
	`, segmentID.UUID())
	if err != nil {
		return fmt.Errorf("recompute ranks for segment %s: %w", segmentID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// WKT helpers
// ---------------------------------------------------------------------------

// pointsToLineStringWKT builds a WKT LINESTRING from GeoPoints. Each
// coordinate is "lon lat" (PostGIS expects longitude first).
func pointsToLineStringWKT(points []domain.GeoPoint) string {
	var b strings.Builder
	b.Grow(20 + len(points)*30)
	b.WriteString("LINESTRING(")
	for i, p := range points {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%g %g", p.Lon, p.Lat)
	}
	b.WriteString(")")
	return b.String()
}

// bboxToPolygonWKT builds a closed WKT POLYGON from a bounding box. The
// first and last vertex coincide as required by OGC SF.
func bboxToPolygonWKT(b port.BoundingBox) string {
	return fmt.Sprintf(
		"POLYGON((%g %g, %g %g, %g %g, %g %g, %g %g))",
		b.MinLon, b.MinLat,
		b.MaxLon, b.MinLat,
		b.MaxLon, b.MaxLat,
		b.MinLon, b.MaxLat,
		b.MinLon, b.MinLat,
	)
}

// prefixedColumns prefixes every column name in a comma-separated list
// with the supplied alias. Used to disambiguate joined queries.
func prefixedColumns(cols string, prefix string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// Param helpers
// ---------------------------------------------------------------------------

func strPtrParam(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func externalAccountIDParam(id *domain.ExternalAccountID) any {
	if id == nil {
		return nil
	}
	return id.UUID()
}

func userIDParam(id *domain.UserID) any {
	if id == nil {
		return nil
	}
	return id.UUID()
}

func climbCategoryParam(c domain.ClimbCategory) any {
	if c == "" {
		return nil
	}
	return string(c)
}

// ---------------------------------------------------------------------------
// Row scanners
// ---------------------------------------------------------------------------

func scanSegmentRow(row rowScanner) (domain.Segment, error) {
	var (
		seg                 domain.Segment
		id                  uuid.UUID
		source              string
		externalAccountID   *uuid.UUID
		ownerUserID         *uuid.UUID
		scope, activityType string
		climbCategory       *string
	)
	if err := row.Scan(
		&id, &source, &seg.ExternalID, &externalAccountID, &ownerUserID,
		&scope, &seg.Name, &seg.Description, &activityType,
		&seg.Polyline, &seg.PolylinePrecision,
		&seg.DistanceM, &seg.ElevationGainM, &seg.ElevationLossM,
		&seg.MinElevationM, &seg.MaxElevationM, &seg.AvgGrade, &seg.MaxGrade,
		&climbCategory,
		&seg.MatchCorridorM, &seg.MatchStartToleranceM, &seg.MatchEndToleranceM,
		&seg.Bidirectional, &seg.Starred,
		&seg.CreatedAt, &seg.UpdatedAt, &seg.CatalogProvider,
	); err != nil {
		return domain.Segment{}, err
	}

	seg.ID = domain.SegmentID(id)
	seg.Source = domain.SegmentSource(source)
	seg.Scope = domain.SegmentScope(scope)
	seg.ActivityType = domain.ActivityType(activityType)
	if externalAccountID != nil {
		ea := domain.ExternalAccountID(*externalAccountID)
		seg.ExternalAccountID = &ea
	}
	if ownerUserID != nil {
		ou := domain.UserID(*ownerUserID)
		seg.OwnerUserID = &ou
	}
	if climbCategory != nil {
		seg.ClimbCategory = domain.ClimbCategory(*climbCategory)
	}
	return seg, nil
}

func scanSegmentEffortRow(row rowScanner) (domain.SegmentEffort, error) {
	var (
		id, segmentID, activityID, sourceID, userID uuid.UUID
		startTime, createdAt                        time.Time
		elapsedS, movingS                           float64
		startOffset, endOffset                      int
		avgHR, maxHR, avgPower, avgCadence          *int
		avgSpeed                                    *float64
		personalRank, instanceRank                  int
		isPersonalRecord, isInstanceRecord          bool
		providerEffortExternalID                    *string
	)
	if err := row.Scan(
		&id, &segmentID, &activityID, &sourceID, &userID,
		&startTime, &elapsedS, &movingS,
		&startOffset, &endOffset,
		&avgHR, &maxHR, &avgPower, &avgCadence, &avgSpeed,
		&personalRank, &instanceRank, &isPersonalRecord, &isInstanceRecord,
		&providerEffortExternalID,
		&createdAt,
	); err != nil {
		return domain.SegmentEffort{}, err
	}
	return domain.SegmentEffort{
		ID:                       domain.SegmentEffortID(id),
		SegmentID:                domain.SegmentID(segmentID),
		ActivityID:               domain.ActivityID(activityID),
		ActivitySourceID:         domain.SourceID(sourceID),
		UserID:                   domain.UserID(userID),
		StartTime:                startTime,
		ElapsedS:                 elapsedS,
		MovingS:                  movingS,
		StartOffset:              startOffset,
		EndOffset:                endOffset,
		AvgHeartRateBpm:          avgHR,
		MaxHeartRateBpm:          maxHR,
		AvgPowerW:                avgPower,
		AvgCadence:               avgCadence,
		AvgSpeedMps:              avgSpeed,
		PersonalRank:             personalRank,
		InstanceRank:             instanceRank,
		IsPersonalRecord:         isPersonalRecord,
		IsInstanceRecord:         isInstanceRecord,
		ProviderEffortExternalID: providerEffortExternalID,
		CreatedAt:                createdAt,
	}, nil
}

// ListUserSegmentsByEffortCount returns the viewer's segments (those they have
// efforts on) ranked by effort count desc — the "most attempted" list for the
// segments landing page. Aggregates per-segment: effort count, the viewer's
// best (min) elapsed time, last effort time, and whether they hold a
// PR / course record.
func (r *SegmentRepo) ListUserSegmentsByEffortCount(
	ctx context.Context,
	userID domain.UserID,
	activityType string, // "" = all
	limit, offset int,
) ([]domain.UserSegmentListItem, error) {
	if limit <= 0 {
		limit = 50
	}
	db := dbtx(ctx, r.pool)
	rows, err := db.Query(ctx,
		`SELECT s.id, s.name, s.activity_type, s.source, s.distance_m,
		        s.elevation_gain_m, s.avg_grade,
		        count(e.*)                       AS effort_count,
		        min(e.elapsed_s)                 AS best_elapsed_s,
		        max(e.start_time)                AS last_effort_at,
		        (s.source = 'strava_catalog' OR bool_or(e.is_personal_record)) AS has_pr,
		        bool_or(e.is_instance_record)    AS has_cr
		   FROM segment_efforts e
		   JOIN segments s ON s.id = e.segment_id
		   JOIN activities a ON a.id = e.activity_id
		  WHERE e.user_id = $1 AND a.deleted_at IS NULL AND `+catalogEffortSQL+communityReadSQL(ctx, "a.")+communitySegmentSQL(ctx, "a.")+`
		    AND ($2 = '' OR s.activity_type = $2)
		  GROUP BY s.id
		  ORDER BY effort_count DESC, last_effort_at DESC, s.id
		  LIMIT $3 OFFSET $4`,
		userID.UUID(), activityType, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list user segments: %w", err)
	}
	defer rows.Close()

	var out []domain.UserSegmentListItem
	for rows.Next() {
		var (
			it     domain.UserSegmentListItem
			id     uuid.UUID
			source string
		)
		if err := rows.Scan(
			&id, &it.Name, &it.ActivityType, &source, &it.DistanceM,
			&it.ElevationGainM, &it.AvgGrade,
			&it.EffortCount, &it.BestElapsedS, &it.LastEffortAt, &it.HasPR, &it.HasCR,
		); err != nil {
			return nil, fmt.Errorf("scan user segment: %w", err)
		}
		it.ID = domain.SegmentID(id)
		it.Source = domain.SegmentSource(source)
		out = append(out, it)
	}
	return out, rows.Err()
}

// UserSegmentStats returns the headline counts for the segments landing page.
func (r *SegmentRepo) UserSegmentStats(ctx context.Context, userID domain.UserID) (domain.UserSegmentStats, error) {
	db := dbtx(ctx, r.pool)
	var st domain.UserSegmentStats
	err := db.QueryRow(ctx,
		`SELECT
		    count(DISTINCT e.segment_id)                                   AS segments,
		    count(*)                                                       AS efforts,
		    count(*) FILTER (WHERE s.source <> 'strava_catalog' AND e.is_personal_record)
		    + count(DISTINCT e.segment_id) FILTER (WHERE s.source = 'strava_catalog') AS prs,
		    count(*) FILTER (WHERE e.is_instance_record)                   AS crs,
		    count(DISTINCT e.segment_id) FILTER (WHERE s.source IN ('external', 'strava_catalog')) AS external,
		    count(DISTINCT e.segment_id) FILTER (WHERE s.source = 'native')   AS native
		   FROM segment_efforts e
		   JOIN segments s ON s.id = e.segment_id
		   JOIN activities a ON a.id = e.activity_id
		  WHERE e.user_id = $1 AND a.deleted_at IS NULL AND `+catalogEffortSQL+communityReadSQL(ctx, "a.")+communitySegmentSQL(ctx, "a."),
		userID.UUID(),
	).Scan(&st.Segments, &st.Efforts, &st.PRs, &st.CRs, &st.External, &st.Native)
	if err != nil {
		return domain.UserSegmentStats{}, fmt.Errorf("user segment stats: %w", err)
	}
	return st, nil
}
