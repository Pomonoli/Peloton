-- +goose Up
-- A stored false/private value lacks provenance. Keep historical choices as written.
ALTER TABLE users ALTER COLUMN profile_is_public SET DEFAULT true;
ALTER TABLE activities ALTER COLUMN privacy SET DEFAULT 'public';
ALTER TABLE user_settings ALTER COLUMN default_activity_privacy SET DEFAULT 'public';

-- +goose StatementBegin
CREATE FUNCTION activity_category_shared(
    owner_id uuid, activity_privacy text, override_policy jsonb, viewer_id uuid, category text
) RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT CASE
        WHEN owner_id = viewer_id THEN true
        WHEN EXISTS (
            SELECT 1 FROM user_blocks
            WHERE (blocker_id = owner_id AND blocked_id = viewer_id)
               OR (blocked_id = owner_id AND blocker_id = viewer_id)
        ) THEN false
        WHEN activity_privacy <> 'public'
          AND (activity_privacy <> 'followers' OR audience.name <> 'followers') THEN false
        WHEN override_policy = '{}'::jsonb THEN false
        WHEN override_policy ? audience.name THEN (override_policy -> audience.name) ? category
        WHEN saved.policy IS NULL THEN true
        WHEN saved.policy = '{}'::jsonb THEN false
        WHEN saved.policy ? audience.name THEN (saved.policy -> audience.name) ? category
        WHEN audience.name = 'public' THEN category = 'summary'
        ELSE category IN ('summary', 'map', 'photos', 'location', 'pace_speed', 'segments', 'best_efforts', 'zones', 'social')
    END
    FROM (SELECT CASE WHEN EXISTS (
        SELECT 1 FROM follows
        WHERE followee_id = owner_id AND follower_id = viewer_id AND status = 'accepted'
    ) THEN 'followers' ELSE 'public' END AS name) audience
    LEFT JOIN user_visibility_defaults saved ON saved.user_id = owner_id;
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION activity_category_shared(uuid, text, jsonb, uuid, text);
ALTER TABLE user_settings ALTER COLUMN default_activity_privacy SET DEFAULT 'private';
ALTER TABLE activities ALTER COLUMN privacy SET DEFAULT 'private';
ALTER TABLE users ALTER COLUMN profile_is_public SET DEFAULT false;
