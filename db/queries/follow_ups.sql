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

-- name: ClaimNote :one
-- The oldest waiting note whose source item is trusted (follow_ups_waiting_notes_idx), with its
-- source's number, locked for the delivering transaction; a note another instance holds is
-- skipped. Only the follow-up is locked, never the source item.
SELECT sqlc.embed(f), s.number AS source_number
FROM follow_ups f
JOIN work_items s ON s.id = f.item_id
WHERE f.state = 'waiting' AND f.target = 'note' AND NOT s.untrusted
ORDER BY f.created_at, f.id
LIMIT 1
FOR UPDATE OF f SKIP LOCKED;

-- name: DeliverFollowUp :one
UPDATE follow_ups SET state = 'delivered', updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: TriageFollowUp :one
-- A note that cannot go where it says: for triage, still waiting, its ref kept in the reason.
UPDATE follow_ups SET target = 'triage', for_factory = NULL, for_number = NULL, reason = sqlc.arg(reason),
  updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: LockNoteTarget :one
-- The item a note's for names, locked for its item_note (FOR NO KEY UPDATE, as LockItem).
SELECT * FROM work_items WHERE factory_id = sqlc.arg(factory_id) AND number = sqlc.arg(number) FOR NO KEY UPDATE;
