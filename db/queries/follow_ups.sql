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
-- An item's follow-ups, oldest first and in output order within a run (UUIDv7 ids), each with the
-- factory and number of the item it was filed as or onto, another factory's included (empty and 0
-- when it is not filed or that item is gone). The join is under row-level security, so another
-- organization's item never resolves.
SELECT sqlc.embed(f), coalesce(w.factory_id::text, '')::text AS filed_factory_id, coalesce(w.number, 0)::bigint AS filed_number
FROM follow_ups f
LEFT JOIN work_items w ON w.id = f.filed_item_id
WHERE f.item_id = sqlc.arg(item_id)
ORDER BY f.created_at, f.id;

-- name: ItemFiledFrom :many
-- The follow-ups filed as or onto an item (follow_ups_filed_item_idx), its sources, oldest first,
-- each with its source item and the item whose landing filed it: the source, or its assembly for
-- a part, as ClaimLandedFollowUp takes it. The joins are under row-level security, so another
-- organization's row or item never resolves.
SELECT f.id, f.station, f.agent, s.id AS source_id, s.factory_id AS source_factory_id, s.number AS source_number,
       l.id AS landed_id, l.factory_id AS landed_factory_id, l.number AS landed_number
FROM follow_ups f
JOIN work_items s ON s.id = f.item_id
JOIN work_items l ON l.id = COALESCE(s.parent_id, s.id)
WHERE f.filed_item_id = sqlc.arg(item_id)
ORDER BY f.created_at, f.id;

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
-- A note or a start that cannot go where it says, or one of untrusted work: for triage, still
-- waiting, its ref kept in the reason.
UPDATE follow_ups SET target = 'triage', for_factory = NULL, for_number = NULL, start = NULL, reason = sqlc.narg(reason),
  updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: LockNoteTarget :one
-- The item a note's for names, locked for its item_note (FOR NO KEY UPDATE, as LockItem).
SELECT * FROM work_items WHERE factory_id = sqlc.arg(factory_id) AND number = sqlc.arg(number) FOR NO KEY UPDATE;

-- name: ClaimLandedFollowUp :one
-- The oldest waiting start or triage follow-up whose work has landed or was cancelled
-- (follow_ups_waiting_filing_idx), or note of an untrusted source, locked; one another instance
-- holds is skipped. Its root is the source item, or its assembly for a part. An untrusted
-- source's follow-ups, its notes too, are never delivered or filed on their own: they go to a
-- triage item of their own when its work lands (ADR 0040 decision 21). Only the follow-ups of
-- factories goald has loaded are taken, since filing needs their line.
SELECT sqlc.embed(f), COALESCE(s.parent_id, s.id)::uuid AS root_id
FROM follow_ups f
JOIN work_items s ON s.id = f.item_id
LEFT JOIN work_items p ON p.id = s.parent_id
WHERE f.state = 'waiting' AND (f.target IN ('start', 'triage') OR (f.target = 'note' AND s.untrusted))
  AND f.factory_id = ANY(sqlc.arg(factories)::uuid[])
  AND (s.state = 'cancelled' OR p.state = 'cancelled' OR COALESCE(p.state, s.state) = 'done')
ORDER BY f.created_at, f.id
LIMIT 1
FOR UPDATE OF f SKIP LOCKED;

-- name: LandedFollowUps :many
-- Every waiting follow-up of a root and its parts that ClaimLandedFollowUp would take, oldest
-- first, locked; the claimed one is already this transaction's.
SELECT f.*
FROM follow_ups f
JOIN work_items s ON s.id = f.item_id
LEFT JOIN work_items p ON p.id = s.parent_id
WHERE f.state = 'waiting' AND (f.target IN ('start', 'triage') OR (f.target = 'note' AND s.untrusted))
  AND COALESCE(s.parent_id, s.id) = sqlc.arg(root_id)::uuid
  AND (s.state = 'cancelled' OR p.state = 'cancelled' OR COALESCE(p.state, s.state) = 'done')
ORDER BY f.created_at, f.id
FOR UPDATE OF f SKIP LOCKED;

-- name: DropFollowUp :one
UPDATE follow_ups SET state = 'dropped', reason = sqlc.arg(reason), updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: FileFollowUpOnto :one
-- A follow-up filed as, or onto, an item: one it became, an open one it duplicates or a triage item.
UPDATE follow_ups SET state = 'filed', filed_item_id = sqlc.arg(filed_item_id), updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: OpenItemFiledFrom :one
-- An open item already filed from a source item's follow-ups whose title is key (trimmed and case
-- folded, domain.FollowUpKey): a start follow-up with that title is filed onto it.
SELECT w.* FROM follow_ups f
JOIN work_items w ON w.id = f.filed_item_id
WHERE f.item_id = sqlc.arg(item_id) AND f.state = 'filed' AND NOT w.triage
  AND w.state NOT IN ('done', 'cancelled')
  AND lower(regexp_replace(btrim(w.title), '\s+', ' ', 'g')) = sqlc.arg(key)::text
ORDER BY w.created_at
LIMIT 1;

-- name: LockFollowUpCap :exec
-- Serializes the follow-up items filed into one factory, and the find or file of its triage item,
-- so two instances cannot both read the cap's count below it or open two triage items. Taken
-- after the follow-ups' locks and before any item's.
SELECT pg_advisory_xact_lock(hashtextextended('goal.follow-ups:' || sqlc.arg(factory_id)::uuid::text, 0));

-- name: FollowUpItemsSince :one
-- How many follow-up items the engine filed into the factory since a time, triage items aside
-- (work_items_follow_up_cap_idx): the daily cap's count.
SELECT count(*) FROM work_items
WHERE factory_id = sqlc.arg(factory_id) AND origin = 'follow-up' AND created_by IS NULL AND NOT triage
  AND created_at > sqlc.arg(since);

-- name: CollectingTriageItem :one
-- The factory's triage item that still collects (work_items_triage_idx): open, with no run yet,
-- and not an untrusted item's own, locked for the follow-ups that join it.
SELECT w.* FROM work_items w
WHERE w.factory_id = sqlc.arg(factory_id) AND w.triage AND NOT w.untrusted AND w.state NOT IN ('done', 'cancelled')
  AND NOT EXISTS (SELECT 1 FROM agent_runs r WHERE r.item_id = w.id)
ORDER BY w.created_at
LIMIT 1
FOR NO KEY UPDATE OF w;

-- name: MarkTriageItem :exec
UPDATE work_items SET triage = true WHERE id = sqlc.arg(id);

-- name: TriageFollowUps :many
-- The follow-ups that joined a triage item, oldest first, with their source item's number, title,
-- type, priority and generation, for its title and brief.
SELECT sqlc.embed(f), s.number AS source_number, s.title AS source_title, s.type AS source_type,
  s.priority AS source_priority, s.generation AS source_generation
FROM follow_ups f
JOIN work_items s ON s.id = f.item_id
WHERE f.filed_item_id = sqlc.arg(item_id) AND f.state = 'filed' AND f.target = 'triage'
ORDER BY f.updated_at, f.created_at, f.id;

-- name: RewriteTriageItem :one
UPDATE work_items SET title = sqlc.arg(title), body = sqlc.arg(body), priority = sqlc.arg(priority)::bigint,
  generation = sqlc.arg(generation)::int, updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: LockItemFollowUp :one
-- One follow-up of an item, locked for a person's File or Drop, with the state of the item it was
-- filed onto ('' when none) and whether that item is a triage item.
SELECT sqlc.embed(f), COALESCE(w.state, '')::text AS filed_state, COALESCE(w.triage, false)::bool AS filed_triage
FROM follow_ups f
LEFT JOIN work_items w ON w.id = f.filed_item_id
WHERE f.id = sqlc.arg(id) AND f.item_id = sqlc.arg(item_id)
FOR UPDATE OF f;

-- name: PauseUntrustedTriageItem :one
-- An untrusted item's own triage item, filed paused (blocked, with its paused event) for a person
-- to check and resume (ADR 0040 decision 21).
UPDATE work_items SET state = 'blocked', updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: TriageQueueItem :one
-- The factory's collecting triage item, as CollectingTriageItem finds it but unlocked, for
-- Intake, with how many follow-ups have joined it (follow_ups_filed_item_idx).
SELECT sqlc.embed(w),
  (SELECT count(*) FROM follow_ups f WHERE f.filed_item_id = w.id AND f.state = 'filed' AND f.target = 'triage')::bigint AS follow_ups
FROM work_items w
WHERE w.factory_id = sqlc.arg(factory_id) AND w.triage AND NOT w.untrusted AND w.state NOT IN ('done', 'cancelled')
  AND NOT EXISTS (SELECT 1 FROM agent_runs r WHERE r.item_id = w.id)
ORDER BY w.created_at
LIMIT 1;

-- name: WaitingTriageSources :many
-- The factory's items with waiting triage follow-ups (follow_ups_factory_idx), trusted ones only:
-- an untrusted item's go to a triage item of their own. Oldest first by their first follow-up.
SELECT s.id, s.number, s.title, count(*)::bigint AS follow_ups
FROM follow_ups f
JOIN work_items s ON s.id = f.item_id
WHERE f.factory_id = sqlc.arg(factory_id) AND f.state = 'waiting' AND f.target = 'triage' AND NOT s.untrusted
GROUP BY s.id, s.number, s.title
ORDER BY min(f.created_at), s.id
LIMIT sqlc.arg(max_items);
