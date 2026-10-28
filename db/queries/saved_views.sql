-- Saved views of the Items page (ports.Store ListSavedViews, CreateSavedView, UpdateSavedView,
-- DeleteSavedView; migration 00074), under row-level security: every statement sees the
-- transaction's organization only.

-- name: ListSavedViews :many
-- The factory's shared views, then the viewer's personal ones, each by name with case ignored. A
-- NULL viewer (an API key or a personal token) sees only the shared views.
SELECT * FROM saved_views
WHERE factory_id = sqlc.arg(factory_id)
  AND (shared OR owner_id = sqlc.narg(viewer)::uuid)
ORDER BY shared DESC, lower(name), id;

-- name: InsertSavedView :one
-- org_id comes from its default, goal_org().
INSERT INTO saved_views (id, factory_id, owner_id, name, shared, filters, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(owner_id), sqlc.arg(name), sqlc.arg(shared),
        sqlc.arg(filters), sqlc.arg(created_at), sqlc.arg(created_at))
RETURNING *;

-- name: UpdateSavedView :one
UPDATE saved_views SET name = sqlc.arg(name), filters = sqlc.arg(filters), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND factory_id = sqlc.arg(factory_id)
RETURNING *;

-- name: DeleteSavedView :execrows
DELETE FROM saved_views WHERE id = sqlc.arg(id) AND factory_id = sqlc.arg(factory_id);
