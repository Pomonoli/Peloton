-- +goose Up
ALTER TABLE users ADD COLUMN territory_color_index bigint GENERATED ALWAYS AS IDENTITY;

CREATE TABLE territory_activity_cache (
    activity_id uuid PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
    source_id uuid NOT NULL,
    source_revision bigint NOT NULL,
    algorithm smallint NOT NULL
);

CREATE TABLE territory_activity_cells (
    activity_id uuid NOT NULL REFERENCES territory_activity_cache(activity_id) ON DELETE CASCADE,
    cell bigint NOT NULL,
    distance_mm bigint NOT NULL CHECK (distance_mm > 0),
    PRIMARY KEY (activity_id, cell)
);

CREATE INDEX activities_territory_window_idx ON activities (start_time, id)
WHERE type = 'ride' AND deleted_at IS NULL AND NOT hidden_by_admin AND NOT provider_restricted
    AND primary_stream_source_id IS NOT NULL;

-- +goose Down
DROP INDEX activities_territory_window_idx;
DROP TABLE territory_activity_cells;
DROP TABLE territory_activity_cache;
ALTER TABLE users DROP COLUMN territory_color_index;
