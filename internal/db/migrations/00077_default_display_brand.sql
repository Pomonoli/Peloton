-- +goose Up
-- An empty name follows the application brand. Keep custom instance names.
ALTER TABLE instance_settings ALTER COLUMN instance_name SET DEFAULT '';
UPDATE instance_settings SET instance_name = '' WHERE instance_name = 'Cairn';

-- Update the built-in help copy without modifying worker-provided metadata.
UPDATE providers
SET meta = jsonb_set(meta, '{config_fields,1,help}', to_jsonb(replace(
    meta #>> '{config_fields,1,help}', 'Cairn signs in', 'this server signs in'
)))
WHERE provider = 'garmin' AND package IS NULL
  AND meta #>> '{config_fields,1,help}' LIKE '%Cairn signs in%';

-- +goose Down
UPDATE providers
SET meta = jsonb_set(meta, '{config_fields,1,help}', to_jsonb(replace(
    meta #>> '{config_fields,1,help}', 'this server signs in', 'Cairn signs in'
)))
WHERE provider = 'garmin' AND package IS NULL
  AND meta #>> '{config_fields,1,help}' LIKE '%this server signs in%';

UPDATE instance_settings SET instance_name = 'Cairn' WHERE instance_name = '';
ALTER TABLE instance_settings ALTER COLUMN instance_name SET DEFAULT 'Cairn';
