-- +goose Up
CREATE TABLE recorded_route_tracks (
    activity_id uuid PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
    source_id uuid NOT NULL,
    source_revision bigint NOT NULL,
    length_m double precision NOT NULL,
    bounds geometry(Geometry, 4326) NOT NULL,
    kind text NOT NULL CHECK (kind IN ('loop', 'out_and_back', 'point_to_point')),
    track jsonb NOT NULL,
    preview jsonb NOT NULL
);
CREATE INDEX recorded_route_tracks_bounds_idx ON recorded_route_tracks USING gist (bounds);
CREATE INDEX recorded_route_tracks_length_idx ON recorded_route_tracks (length_m);

CREATE TABLE recorded_routes (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    anchor_id uuid UNIQUE REFERENCES recorded_route_tracks(activity_id) ON DELETE SET NULL
);
CREATE TABLE recorded_route_matches (
    activity_id uuid PRIMARY KEY REFERENCES recorded_route_tracks(activity_id) ON DELETE CASCADE,
    route_id uuid NOT NULL REFERENCES recorded_routes(id) ON DELETE CASCADE,
    overlap double precision NOT NULL
);
CREATE INDEX recorded_route_matches_route_idx ON recorded_route_matches (route_id, activity_id);

CREATE TABLE recorded_route_queue (
    activity_id uuid PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
    generation bigint NOT NULL DEFAULT 1,
    attempts integer NOT NULL DEFAULT 0,
    live boolean NOT NULL DEFAULT true,
    available_at timestamptz NOT NULL
);
CREATE INDEX recorded_route_queue_ready_idx ON recorded_route_queue (live DESC, available_at, activity_id);

-- +goose StatementBegin
CREATE FUNCTION queue_recorded_route() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'activities' THEN
        IF NEW.type = 'ride' AND NEW.primary_stream_source_id IS NOT NULL
            OR TG_OP = 'UPDATE' AND OLD.type = 'ride' AND OLD.primary_stream_source_id IS NOT NULL THEN
            INSERT INTO recorded_route_queue (activity_id, available_at) VALUES (NEW.id, now())
            ON CONFLICT (activity_id) DO UPDATE
            SET generation = recorded_route_queue.generation + 1, attempts = 0, live = true, available_at = EXCLUDED.available_at;
        END IF;
    ELSE
        INSERT INTO recorded_route_queue (activity_id, available_at)
        SELECT id, now() FROM activities WHERE type = 'ride' AND primary_stream_source_id = NEW.id
        ON CONFLICT (activity_id) DO UPDATE
        SET generation = recorded_route_queue.generation + 1, attempts = 0, live = true, available_at = EXCLUDED.available_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER queue_recorded_route_activity
AFTER INSERT OR UPDATE OF primary_stream_source_id, type, deleted_at, hidden_by_admin, provider_restricted
ON activities FOR EACH ROW EXECUTE FUNCTION queue_recorded_route();

CREATE TRIGGER queue_recorded_route_source
AFTER UPDATE OF heatmap_revision, status ON activity_sources FOR EACH ROW
WHEN (OLD.heatmap_revision IS DISTINCT FROM NEW.heatmap_revision OR OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION queue_recorded_route();

INSERT INTO recorded_route_queue (activity_id, available_at, live)
SELECT id, LEAST(start_time, now()), false FROM activities
WHERE deleted_at IS NULL AND type = 'ride' AND primary_stream_source_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TRIGGER queue_recorded_route_source ON activity_sources;
DROP TRIGGER queue_recorded_route_activity ON activities;
DROP FUNCTION queue_recorded_route();
DROP TABLE recorded_route_queue;
DROP TABLE recorded_route_matches;
DROP TABLE recorded_routes;
DROP TABLE recorded_route_tracks;
