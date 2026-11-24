-- +goose Up
UPDATE users SET profile_is_public = true WHERE NOT profile_is_public;

UPDATE activities
SET privacy = 'public', visibility_override = NULL
WHERE privacy <> 'public' OR visibility_override IS NOT NULL;

UPDATE user_settings
SET default_activity_privacy = 'public'
WHERE default_activity_privacy <> 'public';

-- Reset saved policies to the instance-wide member defaults.
DELETE FROM user_visibility_defaults;

-- Cancel undelivered public announcements of local activities.
UPDATE federation_deliveries
SET status = 'dead', last_error = 'Local activities are restricted to members', updated_at = now()
WHERE status = 'pending' AND body ->> 'type' = 'Create';

-- +goose Down
-- Normalized visibility values cannot be reconstructed from the new values.
SELECT 1;
