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
const sharedActivitySQL = "NOT a.hidden_by_admin AND NOT a.provider_restricted"
const communityActivitySQL = "a.deleted_at IS NULL AND ((" + sharedActivitySQL + ") OR a.user_id = $1)"

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
		return " AND NOT " + prefix + "hidden_by_admin AND NOT " + prefix + "provider_restricted"
	}
	return ""
}
