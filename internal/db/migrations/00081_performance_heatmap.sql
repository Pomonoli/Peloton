-- +goose Up
ALTER TABLE activity_sources ADD COLUMN heatmap_revision bigint NOT NULL DEFAULT 0;

CREATE TABLE performance_heatmap_cache (
    activity_id uuid PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
    signature text NOT NULL,
    has_track boolean NOT NULL
);

CREATE TABLE performance_heatmap_tiles (
    activity_id uuid NOT NULL REFERENCES performance_heatmap_cache(activity_id) ON DELETE CASCADE,
    level smallint NOT NULL,
    tile_x smallint NOT NULL,
    tile_y smallint NOT NULL,
    bins bytea NOT NULL,
    PRIMARY KEY (activity_id, level, tile_x, tile_y)
);

CREATE INDEX performance_heatmap_viewport_idx ON performance_heatmap_tiles (level, tile_x, tile_y, activity_id);

-- Drill-downs constrain these fields before ranking the existing best_efforts.
CREATE INDEX best_efforts_community_bucket_idx ON best_efforts (activity_type, metric, window_kind, window_value);

-- +goose Down
DROP INDEX best_efforts_community_bucket_idx;
DROP TABLE performance_heatmap_tiles;
DROP TABLE performance_heatmap_cache;
ALTER TABLE activity_sources DROP COLUMN heatmap_revision;
