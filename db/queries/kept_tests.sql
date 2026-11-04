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
-- The factory's kept tests, only station's when it is not empty, newest first (kept_tests_factory_idx),
-- each with the verdict, finish and run of its newest finished try of any source (tries_kept_test_idx).
SELECT sqlc.embed(k), w.number AS item_number, w.title AS item_title,
       l.result AS last_result, l.finished_at AS last_at, l.try_run_id AS last_run_id FROM kept_tests k
JOIN work_items w ON w.id = k.item_id
LEFT JOIN LATERAL (
  SELECT t.verdict AS result, t.finished_at, t.try_run_id FROM tries t
  WHERE t.kept_test_id = k.id AND t.state = 'done'
  ORDER BY t.finished_at DESC, t.id DESC
  LIMIT 1
) l ON true
WHERE k.factory_id = sqlc.arg(factory_id) AND (sqlc.arg(station)::text = '' OR k.station = sqlc.arg(station)::text)
ORDER BY k.kept_at DESC, k.id DESC;

-- name: KeptTestByID :one
SELECT sqlc.embed(k), w.number AS item_number, w.title AS item_title,
       l.result AS last_result, l.finished_at AS last_at, l.try_run_id AS last_run_id FROM kept_tests k
JOIN work_items w ON w.id = k.item_id
LEFT JOIN LATERAL (
  SELECT t.verdict AS result, t.finished_at, t.try_run_id FROM tries t
  WHERE t.kept_test_id = k.id AND t.state = 'done'
  ORDER BY t.finished_at DESC, t.id DESC
  LIMIT 1
) l ON true
WHERE k.id = sqlc.arg(id);

-- name: DeleteKeptTest :execrows
DELETE FROM kept_tests WHERE factory_id = sqlc.arg(factory_id) AND id = sqlc.arg(id);
