-- +goose Up
CREATE TABLE training_stress_backfill (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    owner_cursor uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    activity_cursor uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    completed boolean NOT NULL DEFAULT false,
    scanned bigint NOT NULL DEFAULT 0,
    repaired bigint NOT NULL DEFAULT 0,
    missing bigint NOT NULL DEFAULT 0,
    missing_with_duration bigint NOT NULL DEFAULT 0
);
INSERT INTO training_stress_backfill DEFAULT VALUES;

-- +goose Down
DROP TABLE training_stress_backfill;
