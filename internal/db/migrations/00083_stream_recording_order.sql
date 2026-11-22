-- +goose Up
ALTER TABLE activity_streams ADD COLUMN recording_index integer;

-- +goose Down
ALTER TABLE activity_streams DROP COLUMN recording_index;
