-- name: SaveFactory :exec
-- Registers a loaded factory definition, or updates the row when the definition changed.
INSERT INTO factories (id, name, title, definition, definition_sha256)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.narg(title), sqlc.arg(definition)::jsonb, sqlc.arg(definition_sha256)::text)
ON CONFLICT (id) DO UPDATE SET
  name = EXCLUDED.name, title = EXCLUDED.title, definition = EXCLUDED.definition,
  definition_sha256 = EXCLUDED.definition_sha256, updated_at = now()
WHERE (factories.name, factories.title, factories.definition_sha256)
  IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.title, EXCLUDED.definition_sha256);
