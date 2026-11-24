package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Keep directory totals, the recent ride link and the profile history on the same visibility rule.
const communityActivitySQL = "a.deleted_at IS NULL AND ((NOT a.hidden_by_admin AND NOT a.provider_restricted) OR a.user_id = $1)" +
	" AND activity_category_shared(a.user_id, a.privacy, a.visibility_override, $1, 'summary')"

func (r *ActivityRepo) ListCommunityMembers(ctx context.Context, viewer domain.UserID) ([]domain.CommunityMember, error) {
	rows, err := dbtx(ctx, r.pool).Query(ctx, `
		SELECT u.id, u.username, u.display_name, COALESCE(u.avatar_url, ''),
		       stats.activity_count, stats.cycling_distance_m, ride.id, ride.title, ride.start_time
		FROM users u
		CROSS JOIN LATERAL (
			SELECT count(*) AS activity_count,
			       COALESCE(sum(a.distance_m) FILTER (WHERE a.type = $2
					AND a.distance_m > 0 AND a.distance_m < 'Infinity'::float8), 0) AS cycling_distance_m
			FROM activities a WHERE a.user_id = u.id AND `+communityActivitySQL+`
		) stats
		LEFT JOIN LATERAL (
			SELECT a.id, a.title, a.start_time
			FROM activities a WHERE a.user_id = u.id AND a.type = $2 AND `+communityActivitySQL+`
			ORDER BY a.start_time DESC, a.id DESC LIMIT 1
		) ride ON true
		WHERE u.status <> 'deleted'
		ORDER BY lower(COALESCE(NULLIF(u.display_name, ''), u.username::text)), u.username`,
		viewer.UUID(), string(domain.ActivityTypeRide))
	if err != nil {
		return nil, fmt.Errorf("query community directory: %w", err)
	}
	return pgCollectRows(rows, func(row rowScanner) (domain.CommunityMember, error) {
		var member domain.CommunityMember
		var uid uuid.UUID
		var rideID *uuid.UUID
		var rideTitle *string
		var rideTime *time.Time
		err := row.Scan(&uid, &member.Username, &member.DisplayName, &member.AvatarURL,
			&member.ActivityCount, &member.CyclingDistanceM, &rideID, &rideTitle, &rideTime)
		if err != nil {
			return member, err
		}
		member.UserID = domain.UserID(uid)
		if rideID != nil {
			member.LatestRide = &domain.CommunityRide{ID: domain.ActivityID(*rideID), Title: *rideTitle, StartTime: *rideTime}
		}
		return member, nil
	}, "scan community member", "iterate community directory")
}

// communityReadSQL applies to a subject's activity queries after a distinct viewer was authorized.
func communityReadSQL(ctx context.Context, prefix string) string {
	if port.IsCommunityActivityRead(ctx) {
		return " AND NOT " + prefix + "hidden_by_admin AND NOT " + prefix + "provider_restricted" +
			" AND " + sharedCategorySQL(prefix, communityViewerSQL(ctx), "'summary'")
	}
	return ""
}

// The viewer expression contains only a UUID parsed by the auth layer.
// A missing viewer resolves the public audience.
func communityViewerSQL(ctx context.Context) string {
	if viewer, ok := port.CommunityViewer(ctx); ok {
		return "'" + viewer.String() + "'::uuid"
	}
	return "NULL::uuid"
}

func sharedCategorySQL(prefix, viewer, category string) string {
	return "activity_category_shared(" + prefix + "user_id, " + prefix + "privacy, " +
		prefix + "visibility_override, " + viewer + ", " + category + ")"
}

const recordMetricCategorySQL = `CASE b.metric
	WHEN 'power' THEN 'power'
	WHEN 'heart_rate' THEN 'hr'
	WHEN 'pace' THEN 'pace_speed'
	WHEN 'speed' THEN 'pace_speed'
	ELSE 'best_efforts'
END`

func sharedRecordSQL(prefix, viewer string) string {
	return " AND " + sharedCategorySQL(prefix, viewer, "'best_efforts'") +
		" AND " + sharedCategorySQL(prefix, viewer, recordMetricCategorySQL)
}

func communityRecordSQL(ctx context.Context, prefix string) string {
	if !port.IsCommunityActivityRead(ctx) {
		return ""
	}
	return sharedRecordSQL(prefix, communityViewerSQL(ctx))
}

func communitySegmentSQL(ctx context.Context, prefix string) string {
	if !port.IsCommunityActivityRead(ctx) {
		return ""
	}
	return " AND " + sharedCategorySQL(prefix, communityViewerSQL(ctx), "'segments'")
}

// Precomputed routes contain original coordinates. Use the unmasked
// cache only where the athlete has no privacy zones.
func communityMapSQL(ctx context.Context, prefix string) string {
	if !port.IsCommunityActivityRead(ctx) {
		return ""
	}
	viewer := communityViewerSQL(ctx)
	return " AND " + sharedCategorySQL(prefix, viewer, "'map'") +
		" AND " + sharedCategorySQL(prefix, viewer, "'pace_speed'") +
		" AND NOT EXISTS (SELECT 1 FROM privacy_zones z WHERE z.user_id = " + prefix + "user_id)"
}
