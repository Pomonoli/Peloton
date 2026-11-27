package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

func (r *ActivityRepo) ListSpeedDistance(ctx context.Context, rider domain.UserID, filter port.ActivityListFilter, year int) ([]port.SpeedDistancePoint, error) {
	if filter.Type != "" && filter.Type != string(domain.ActivityTypeRide) {
		return nil, nil
	}
	filter.Type = string(domain.ActivityTypeRide)
	conditions, args := activityFilterConds(rider, filter, "")
	where := strings.Join(conditions, " AND ") + communityReadSQL(ctx, "")
	if year > 0 {
		// Match ActivityYearStats' database calendar, while leaving start_time
		// indexable and applying any independent from/to filters as well.
		args = append(args, year)
		where += fmt.Sprintf(" AND start_time >= make_timestamptz($%[1]d, 1, 1, 0, 0, 0) AND start_time < make_timestamptz($%[1]d + 1, 1, 1, 0, 0, 0)", len(args))
	}
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		SELECT id, title, start_time, distance_m, moving_duration_s
		FROM activities WHERE `+where+`
			AND distance_m > 0 AND distance_m < 'Infinity'::float8
			AND moving_duration_s > 0 AND moving_duration_s <= elapsed_duration_s
			AND moving_duration_s <= 9223372036
		ORDER BY start_time, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list cycling distances and speeds: %w", err)
	}
	defer rows.Close()
	var out []port.SpeedDistancePoint
	for rows.Next() {
		var point port.SpeedDistancePoint
		var id uuid.UUID
		var seconds int64
		if err := rows.Scan(&id, &point.Title, &point.Start, &point.DistanceM, &seconds); err != nil {
			return nil, fmt.Errorf("scan cycling distance and speed: %w", err)
		}
		speed := domain.ActivityAverageSpeed(&point.DistanceM, time.Duration(seconds)*time.Second)
		if speed != nil && *speed <= domain.CyclingMaxSpeedMps {
			point.ID, point.SpeedMps = domain.ActivityID(id), *speed
			out = append(out, point)
		}
	}
	return out, rows.Err()
}

const sharedRideWindowSQL = `tstzrange(a.start_time, a.end_time, '[]')`

const sharedRideActivitiesSQL = `
	FROM activities a
	JOIN activity_sources src ON src.id = a.primary_stream_source_id AND src.activity_id = a.id
	WHERE a.user_id = %s AND a.type = 'ride' AND NOT a.is_virtual AND a.deleted_at IS NULL
		AND a.primary_stream_source_id IS NOT NULL
		AND ((NOT a.hidden_by_admin AND NOT a.provider_restricted) OR a.user_id = $3)
		AND ` + comparisonSourceSQL

func (r *ActivityRepo) ListSharedRidePairs(
	ctx context.Context, viewer, first, second domain.UserID, from, to time.Time, visibility int16, maskKey string,
) ([]port.SharedRidePair, error) {
	var start, end *time.Time
	if !from.IsZero() {
		start = &from
	}
	if !to.IsZero() {
		end = &to
	}
	// Include intervals overlapping the requested range. A pair's cache
	// contains the result for the entire overlap, regardless of date filters.
	const columns = `a.id, a.title, a.start_time, a.end_time, src.id AS source_id, src.heatmap_revision AS revision,` + sharedRideWindowSQL + ` AS window`
	selection := func(owner string) string {
		return "SELECT " + columns + fmt.Sprintf(sharedRideActivitiesSQL, owner) +
			" AND " + sharedRideWindowSQL + " && tstzrange($4, $5, '[)')"
	}
	// Source IDs and the stream-write revision invalidate the cache on
	// primary source changes, replacement, or deletion. Bump v1 when the
	// proximity algorithm or the serialized result changes.
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		WITH candidates AS (
			SELECT f.id AS first_id, f.title AS first_title, f.start_time AS first_start, f.end_time AS first_end,
				f.source_id AS first_source, f.revision AS first_revision,
				s.id AS second_id, s.title AS second_title, s.start_time AS second_start, s.end_time AS second_end,
				s.source_id AS second_source, s.revision AS second_revision,
				md5(concat_ws('|', 'v1', $7::text, f.source_id, f.revision, f.window, s.source_id, s.revision, s.window)) AS signature
			FROM (`+selection("$1")+`) f JOIN (`+selection("$2")+`) s ON f.window && s.window
		)
		SELECT p.first_id, p.first_title, p.first_start, p.first_end, p.first_source, p.first_revision,
			p.second_id, p.second_title, p.second_start, p.second_end, p.second_source, p.second_revision,
			p.signature, c.proximity
		FROM candidates p LEFT JOIN shared_ride_cache c
			ON c.first_activity_id = p.first_id AND c.second_activity_id = p.second_id
				AND c.visibility = $6 AND c.signature = p.signature
		ORDER BY p.first_start, p.first_id, p.second_start, p.second_id`,
		first.UUID(), second.UUID(), viewer.UUID(), start, end, visibility, maskKey)
	if err != nil {
		return nil, fmt.Errorf("list overlapping rides: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (port.SharedRidePair, error) {
		var p port.SharedRidePair
		var a, b, as, bs uuid.UUID
		err := row.Scan(
			&a, &p.First.Title, &p.First.Start, &p.First.End, &as, &p.First.Revision,
			&b, &p.Second.Title, &p.Second.Start, &p.Second.End, &bs, &p.Second.Revision,
			&p.Signature, &p.Cached,
		)
		p.First.ID, p.First.Source = domain.ActivityID(a), domain.SourceID(as)
		p.Second.ID, p.Second.Source = domain.ActivityID(b), domain.SourceID(bs)
		return p, err
	}, "scan overlapping ride", "iterate overlapping rides")
}

func (r *ActivityRepo) ReadSharedRideTrack(ctx context.Context, recording port.SharedRideRecording) ([]domain.StreamSample, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		SELECT ts, COALESCE(recording_index, 0), latitude, longitude, distance_m, speed_mps
		FROM activity_streams WHERE activity_source_id = $1 AND ts BETWEEN $2 AND $3
		ORDER BY ts`, recording.Source.UUID(), recording.Start, recording.End)
	if err != nil {
		return nil, fmt.Errorf("read timed GPS track: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (domain.StreamSample, error) {
		var s domain.StreamSample
		err := row.Scan(&s.Timestamp, &s.RecordingIndex, &s.Latitude, &s.Longitude, &s.DistanceM, &s.SpeedMps)
		return s, err
	}, "scan timed GPS sample", "iterate timed GPS samples")
}

func (r *ActivityRepo) SaveSharedRidePair(ctx context.Context, pair port.SharedRidePair, visibility int16, proximity domain.RideProximity) error {
	_, err := dbtx(ctx, r.pool).Exec(ctx, `
		INSERT INTO shared_ride_cache (first_activity_id, second_activity_id, visibility, signature, proximity)
		SELECT a.id, b.id, $3, $4, $5
		FROM activities a JOIN activity_sources sa ON sa.id = a.primary_stream_source_id
		CROSS JOIN activities b JOIN activity_sources sb ON sb.id = b.primary_stream_source_id
		WHERE a.id = $1 AND b.id = $2 AND sa.id = $6 AND sb.id = $7
			AND sa.heatmap_revision = $8 AND sb.heatmap_revision = $9
			AND a.start_time = $10 AND a.end_time = $11 AND b.start_time = $12 AND b.end_time = $13
		ON CONFLICT (first_activity_id, second_activity_id, visibility)
		DO UPDATE SET signature = EXCLUDED.signature, proximity = EXCLUDED.proximity
		WHERE shared_ride_cache.signature <> EXCLUDED.signature`,
		pair.First.ID.UUID(), pair.Second.ID.UUID(), visibility, pair.Signature, proximity,
		pair.First.Source.UUID(), pair.Second.Source.UUID(), pair.First.Revision, pair.Second.Revision,
		pair.First.Start, pair.First.End, pair.Second.Start, pair.Second.End)
	if err != nil {
		return fmt.Errorf("cache shared ride pair: %w", err)
	}
	return nil
}
