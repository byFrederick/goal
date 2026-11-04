-- Kept tests (00105, ADR 0039 decision 12). Row-level security scopes every query to the
-- organization.

-- name: InsertKeptTest :one
-- The item must be the factory's: no row when it is not.
INSERT INTO kept_tests (id, factory_id, item_id, station, kept_by, kept_at)
SELECT sqlc.arg(id), w.factory_id, w.id, sqlc.arg(station), sqlc.arg(kept_by), sqlc.arg(kept_at)
FROM work_items w
WHERE w.id = sqlc.arg(item_id) AND w.factory_id = sqlc.arg(factory_id)
RETURNING id;

-- name: KeptTests :many
-- The factory's kept tests, only station's when it is not empty, newest first (kept_tests_factory_idx).
SELECT sqlc.embed(k), w.number AS item_number, w.title AS item_title FROM kept_tests k
JOIN work_items w ON w.id = k.item_id
WHERE k.factory_id = sqlc.arg(factory_id) AND (sqlc.arg(station)::text = '' OR k.station = sqlc.arg(station)::text)
ORDER BY k.kept_at DESC, k.id DESC;

-- name: KeptTestByID :one
SELECT sqlc.embed(k), w.number AS item_number, w.title AS item_title FROM kept_tests k
JOIN work_items w ON w.id = k.item_id
WHERE k.id = sqlc.arg(id);

-- name: DeleteKeptTest :execrows
DELETE FROM kept_tests WHERE factory_id = sqlc.arg(factory_id) AND id = sqlc.arg(id);
