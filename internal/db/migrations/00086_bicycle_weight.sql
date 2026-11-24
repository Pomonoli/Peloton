-- +goose Up
ALTER TABLE gear ADD COLUMN weight_kg double precision
    CHECK (weight_kg IS NULL OR (kind = 'bike' AND weight_kg BETWEEN 2 AND 100));

-- +goose Down
ALTER TABLE gear DROP COLUMN weight_kg;
