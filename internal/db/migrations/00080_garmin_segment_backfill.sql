-- +goose Up
ALTER TABLE activity_sources ADD COLUMN garmin_segments_scanned_at timestamptz;

CREATE INDEX activity_sources_garmin_segments_pending_idx ON activity_sources (id)
    WHERE provider = 'garmin' AND status = 'active'
      AND parsed @> '{"has_stream":true}' AND garmin_segments_scanned_at IS NULL;

-- +goose Down
DROP INDEX activity_sources_garmin_segments_pending_idx;
ALTER TABLE activity_sources DROP COLUMN garmin_segments_scanned_at;
