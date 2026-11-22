-- +goose Up
ALTER TABLE activities ADD COLUMN provider_restricted boolean NOT NULL DEFAULT false;

-- Quarantine older Strava aggregates until the normal merge pipeline can
-- rebuild them from independent sources. The original recordings stay intact.
UPDATE activities a SET provider_restricted = true
WHERE EXISTS (SELECT 1 FROM activity_sources s WHERE s.activity_id = a.id AND s.provider = 'strava');

ALTER TABLE best_efforts ALTER CONSTRAINT best_efforts_activity_id_fkey DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE user_records ALTER CONSTRAINT user_records_activity_id_fkey DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE segment_efforts ALTER CONSTRAINT segment_efforts_activity_id_fkey DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE activity_attachments ALTER CONSTRAINT activity_attachments_activity_id_fkey DEFERRABLE INITIALLY DEFERRED;

-- +goose Down
ALTER TABLE best_efforts ALTER CONSTRAINT best_efforts_activity_id_fkey NOT DEFERRABLE;
ALTER TABLE user_records ALTER CONSTRAINT user_records_activity_id_fkey NOT DEFERRABLE;
ALTER TABLE segment_efforts ALTER CONSTRAINT segment_efforts_activity_id_fkey NOT DEFERRABLE;
ALTER TABLE activity_attachments ALTER CONSTRAINT activity_attachments_activity_id_fkey NOT DEFERRABLE;
ALTER TABLE activities DROP COLUMN provider_restricted;
