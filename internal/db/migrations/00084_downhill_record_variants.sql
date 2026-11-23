-- +goose Up
ALTER TABLE best_efforts
    ADD COLUMN downhill_excluded boolean NOT NULL DEFAULT false,
    DROP CONSTRAINT best_efforts_activity_source_id_metric_window_kind_window_v_key,
    ADD CONSTRAINT best_efforts_source_window_variant_key
        UNIQUE (activity_source_id, metric, window_kind, window_value, downhill_excluded);

-- +goose Down
DELETE FROM best_efforts WHERE downhill_excluded;
ALTER TABLE best_efforts
    DROP CONSTRAINT best_efforts_source_window_variant_key,
    DROP COLUMN downhill_excluded,
    ADD UNIQUE (activity_source_id, metric, window_kind, window_value);
