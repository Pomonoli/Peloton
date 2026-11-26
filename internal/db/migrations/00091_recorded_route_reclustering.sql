-- +goose Up
CREATE TABLE recorded_route_recluster (pending boolean PRIMARY KEY DEFAULT true CHECK (pending));
INSERT INTO recorded_route_recluster DEFAULT VALUES;

CREATE INDEX recorded_routes_orphan_idx ON recorded_routes (id) WHERE anchor_id IS NULL;

-- +goose StatementBegin
CREATE FUNCTION queue_recorded_route_hard_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO recorded_route_queue (activity_id, available_at)
    SELECT peer.activity_id, now()
    FROM recorded_route_matches removed
    JOIN recorded_route_matches peer USING (route_id)
    JOIN activities a ON a.id = peer.activity_id AND a.deleted_at IS NULL
    WHERE removed.activity_id = OLD.id AND peer.activity_id <> OLD.id
    ORDER BY peer.activity_id LIMIT 1
    ON CONFLICT (activity_id) DO UPDATE
    SET generation = recorded_route_queue.generation + 1, attempts = 0, live = true, available_at = EXCLUDED.available_at;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER queue_recorded_route_hard_delete
BEFORE DELETE ON activities FOR EACH ROW EXECUTE FUNCTION queue_recorded_route_hard_delete();

-- +goose StatementBegin
CREATE FUNCTION queue_recorded_route_user() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO recorded_route_queue (activity_id, available_at)
    SELECT id, now() FROM activities WHERE user_id = NEW.id AND type = 'ride'
        AND deleted_at IS NULL AND primary_stream_source_id IS NOT NULL
    ORDER BY id
    ON CONFLICT (activity_id) DO UPDATE
    SET generation = recorded_route_queue.generation + 1, attempts = 0, live = true, available_at = EXCLUDED.available_at;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER queue_recorded_route_user
AFTER UPDATE OF status ON users FOR EACH ROW
WHEN ((OLD.status = 'deleted') IS DISTINCT FROM (NEW.status = 'deleted'))
EXECUTE FUNCTION queue_recorded_route_user();

-- +goose Down
DROP TRIGGER queue_recorded_route_user ON users;
DROP FUNCTION queue_recorded_route_user();
DROP TRIGGER queue_recorded_route_hard_delete ON activities;
DROP FUNCTION queue_recorded_route_hard_delete();
DROP INDEX recorded_routes_orphan_idx;
DROP TABLE recorded_route_recluster;
