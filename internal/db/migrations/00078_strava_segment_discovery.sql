-- +goose Up
ALTER TABLE segments DROP CONSTRAINT segments_source_check;
ALTER TABLE segments ADD CONSTRAINT segments_source_check
    CHECK (source IN ('external', 'native', 'strava_catalog'));
ALTER TABLE segments ADD COLUMN catalog_provider text;
ALTER TABLE segments ADD CONSTRAINT segments_catalog_shape_chk CHECK (
    (source = 'strava_catalog' AND external_id IS NOT NULL AND external_id ~ '^[1-9][0-9]*$'
        AND external_account_id IS NULL AND owner_user_id IS NULL AND scope = 'instance'
        AND activity_type = 'ride' AND NOT bidirectional AND catalog_provider IS NOT NULL)
    OR (source <> 'strava_catalog' AND catalog_provider IS NULL)
);
CREATE UNIQUE INDEX segments_strava_catalog_id_idx ON segments (external_id) WHERE source = 'strava_catalog';

CREATE TABLE segment_discovery_windows (
    provider text NOT NULL,
    depth integer NOT NULL,
    tile_x integer NOT NULL,
    tile_y integer NOT NULL,
    fetched_at timestamptz NOT NULL,
    saturated boolean NOT NULL,
    PRIMARY KEY (provider, depth, tile_x, tile_y)
);

-- +goose Down
DROP TABLE segment_discovery_windows;
DELETE FROM segments WHERE source = 'strava_catalog';
DROP INDEX segments_strava_catalog_id_idx;
ALTER TABLE segments DROP CONSTRAINT segments_catalog_shape_chk;
ALTER TABLE segments DROP COLUMN catalog_provider;
ALTER TABLE segments DROP CONSTRAINT segments_source_check;
ALTER TABLE segments ADD CONSTRAINT segments_source_check CHECK (source IN ('external', 'native'));
