-- Factory definitions: blobs, versions and the current-version pointer (postgres.Definitions).
--
-- Lock order, to rule out deadlocks: the factory row (LockFactory), then its versions, then its
-- items. Factory locks use FOR NO KEY UPDATE, so foreign-key checks from concurrent item and
-- event inserts never wait on them. org_id always comes from its default, goal_org().

-- name: InsertBlobs :exec
-- Records blobs whose objects are in the bucket; a blob the organization has stays as it is.
INSERT INTO definition_blobs (sha256, size)
SELECT unnest(sqlc.arg(hashes)::text[]), unnest(sqlc.arg(sizes)::integer[])
ON CONFLICT (org_id, sha256) DO NOTHING;

-- name: BlobSizes :many
SELECT sha256, size FROM definition_blobs WHERE sha256 = ANY (sqlc.arg(hashes)::text[]);

-- name: InsertFactory :exec
INSERT INTO factories (id, name, title, created_by)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.narg(title), sqlc.narg(created_by));

-- name: FactoryRecord :one
SELECT * FROM factory_records WHERE id = $1;

-- name: FactoryRecords :many
SELECT * FROM factory_records ORDER BY lower(name) COLLATE "C", id;

-- name: LockFactory :one
SELECT id, name, current_version_id, last_version_number, paused_at FROM factories WHERE id = $1 FOR NO KEY UPDATE;

-- name: NameTaken :one
-- Whether another factory of the organization has name, compared without case.
SELECT EXISTS (SELECT 1 FROM factories WHERE lower(name) = lower(sqlc.arg(name)::text) AND id <> sqlc.arg(id));

-- name: DeleteFactory :exec
DELETE FROM factories WHERE id = $1;

-- name: SetFactoryPaused :exec
UPDATE factories SET
  paused_at = CASE WHEN sqlc.arg(paused)::boolean THEN now() END,
  paused_by = CASE WHEN sqlc.arg(paused)::boolean THEN sqlc.arg(paused_by)::text END,
  updated_at = now()
WHERE id = sqlc.arg(id);

-- name: PointFactory :exec
-- Points a factory at a published version, with that version's name and title. A publish raises
-- last_version_number to the new number; a restore names an older one and leaves it. pause pauses
-- the factory (a first publish that does not start it).
UPDATE factories SET
  current_version_id = sqlc.arg(version_id),
  last_version_number = greatest(last_version_number, sqlc.arg(number)::integer),
  name = sqlc.arg(name),
  title = sqlc.narg(title),
  paused_at = CASE WHEN sqlc.arg(pause)::boolean THEN now() ELSE paused_at END,
  paused_by = CASE WHEN sqlc.arg(pause)::boolean THEN sqlc.arg(paused_by)::text ELSE paused_by END,
  updated_at = now()
WHERE id = sqlc.arg(id);

-- name: InsertVersion :one
INSERT INTO definition_versions (
  id, factory_id, state, number, base_version_id, manifest, message, source, created_by, updated_by,
  published_by, published_at
) VALUES (
  sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(state), sqlc.narg(number), sqlc.narg(base_version_id),
  sqlc.arg(manifest), sqlc.arg(message), sqlc.arg(source), sqlc.arg(created_by), sqlc.arg(created_by),
  sqlc.narg(published_by), CASE WHEN sqlc.arg(state) = 'published' THEN now() END
)
RETURNING *;

-- name: GetVersion :one
SELECT * FROM definition_versions WHERE id = $1;

-- name: PublishedVersion :one
-- A factory's published version, by id.
SELECT * FROM definition_versions WHERE factory_id = $1 AND id = $2 AND state = 'published';

-- name: VersionByNumber :one
SELECT * FROM definition_versions WHERE factory_id = $1 AND number = $2;

-- name: Draft :one
SELECT * FROM definition_versions WHERE factory_id = $1 AND state = 'draft';

-- name: LockDraft :one
-- FOR UPDATE, not NO KEY: publishing sets number, a key column.
SELECT * FROM definition_versions WHERE factory_id = $1 AND state = 'draft' FOR UPDATE;

-- name: ListVersions :many
-- Published versions, newest first, paged by number; a non-positive before means from the newest.
SELECT * FROM definition_versions
WHERE factory_id = sqlc.arg(factory_id) AND state = 'published'
  AND (sqlc.arg(before)::integer <= 0 OR number < sqlc.arg(before)::integer)
ORDER BY number DESC
LIMIT sqlc.arg(max_versions)::bigint;

-- name: CurrentVersions :many
SELECT v.* FROM factories f JOIN definition_versions v ON v.id = f.current_version_id ORDER BY f.id;

-- name: SaveDraft :one
UPDATE definition_versions SET
  manifest = sqlc.arg(manifest), revision = revision + 1, updated_by = sqlc.arg(updated_by), updated_at = now()
WHERE id = sqlc.arg(id) AND state = 'draft'
RETURNING *;

-- name: ArchiveDraft :one
UPDATE definition_versions SET state = 'archived', updated_by = sqlc.arg(updated_by), updated_at = now()
WHERE id = sqlc.arg(id) AND state = 'draft'
RETURNING *;

-- name: PublishDraft :one
UPDATE definition_versions SET
  state = 'published', number = sqlc.arg(number), message = sqlc.arg(message),
  published_by = sqlc.arg(published_by)::text, published_at = now(),
  updated_by = sqlc.arg(published_by)::text, updated_at = now()
WHERE id = sqlc.arg(id) AND state = 'draft'
RETURNING *;

-- name: OpenItemsAt :many
-- A factory's open items at stations, locked (item locks use FOR NO KEY UPDATE, as everywhere).
SELECT id, number, station, state FROM work_items
WHERE factory_id = sqlc.arg(factory_id) AND station = ANY (sqlc.arg(stations)::text[])
  AND state NOT IN ('done', 'cancelled')
ORDER BY number
FOR NO KEY UPDATE;

-- name: MoveOffStation :many
-- Moves a factory's open items off a removed station at the caller's time (at), as every other
-- change of an item, so their spans keep one clock. Blocked items stay blocked; items in progress
-- are refused before this runs.
UPDATE work_items SET
  station = sqlc.arg(to_station),
  state = CASE WHEN state = 'blocked' THEN state ELSE sqlc.arg(state)::text END,
  updated_at = sqlc.arg(at)::timestamptz
WHERE factory_id = sqlc.arg(factory_id) AND station = sqlc.arg(from_station)
  AND state NOT IN ('done', 'cancelled', 'in_progress')
RETURNING id, number, updated_at;
