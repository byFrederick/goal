-- Follow-ups (00114, ADR 0040 decisions 5 and 6). Row-level security scopes every query to the
-- organization.

-- name: KeepFollowUp :exec
-- A text already kept for the item (follow_ups_text_key) is skipped, so a rerun or a retried
-- transaction keeps it once.
INSERT INTO follow_ups (
  id, factory_id, item_id, run_id, station, agent, text, text_key, target, for_factory, for_number,
  start, state, reason, created_at, updated_at
) VALUES (
  sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(item_id), sqlc.narg(run_id), sqlc.arg(station), sqlc.arg(agent),
  sqlc.arg(text), sqlc.arg(text_key), sqlc.narg(target), sqlc.narg(for_factory), sqlc.narg(for_number),
  sqlc.narg(start), sqlc.arg(state), sqlc.narg(reason), sqlc.arg(created_at), sqlc.arg(created_at)
)
ON CONFLICT (org_id, item_id, text_key) DO NOTHING;

-- name: ItemFollowUps :many
-- An item's follow-ups, oldest first and in output order within a run (UUIDv7 ids).
SELECT * FROM follow_ups WHERE item_id = sqlc.arg(item_id) ORDER BY created_at, id;
