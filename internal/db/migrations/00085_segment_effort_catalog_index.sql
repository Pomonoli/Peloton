-- +goose Up
CREATE INDEX segment_efforts_segment_activity_source_idx
    ON segment_efforts (segment_id, activity_id, activity_source_id);

-- +goose Down
DROP INDEX segment_efforts_segment_activity_source_idx;
