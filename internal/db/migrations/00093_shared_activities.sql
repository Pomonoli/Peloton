-- +goose Up
-- Index existing and future ride windows for the temporal join. Historical
-- pairs are evaluated from their stored streams and cached on first use.
CREATE INDEX activities_shared_ride_window_idx
    ON activities USING gist (tstzrange(start_time, end_time, '[]'))
    WHERE type = 'ride' AND deleted_at IS NULL AND NOT is_virtual
        AND primary_stream_source_id IS NOT NULL;

CREATE TABLE shared_ride_cache (
    first_activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    second_activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    visibility smallint NOT NULL CHECK (visibility BETWEEN 0 AND 2),
    signature text NOT NULL,
    proximity jsonb NOT NULL,
    PRIMARY KEY (first_activity_id, second_activity_id, visibility)
);
CREATE INDEX shared_ride_cache_second_idx ON shared_ride_cache (second_activity_id);

-- +goose Down
DROP TABLE shared_ride_cache;
DROP INDEX activities_shared_ride_window_idx;
