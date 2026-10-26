-- Human touches per item (ports.FlowStats, Store.LabelTouch; spec 4 §4.9, §4.10), read through
-- the item_touches view of migration 00052. Every query runs under the transaction's
-- organization (row-level security) and reads touches by item, which the view's DISTINCT ON
-- takes as an index condition (item_events_person_idx).

-- name: ItemsTouches :many
-- The touches of the items, by item, in time order.
SELECT * FROM item_touches WHERE item_id = ANY(sqlc.arg(item_ids)::uuid[]) ORDER BY item_id, at, event_id;

-- name: TouchCounts :many
SELECT item_id, count(*) AS touches FROM item_touches
WHERE item_id = ANY(sqlc.arg(item_ids)::uuid[])
GROUP BY item_id;

-- name: FinishedItems :many
-- The items of the factories, of the type, done in [since, until) by when they were done
-- (work_items_done_at_idx), with the definition version of each one's last agent run
-- (agent_runs_item_id_idx), kept to one version when given.
SELECT * FROM (
  SELECT w.id AS item_id, w.factory_id, w.done_at::timestamptz AS done_at,
         (SELECT r.definition_version_id FROM agent_runs r WHERE r.item_id = w.id
          ORDER BY r.started_at DESC, r.id DESC LIMIT 1) AS version_id
  FROM work_items w
  WHERE w.factory_id = ANY(sqlc.arg(factories)::uuid[]) AND w.done_at >= sqlc.arg(since)::timestamptz
    AND w.done_at < sqlc.arg(until)::timestamptz
    AND (sqlc.arg(type)::text = '' OR w.type = sqlc.arg(type)::text)
) done
WHERE sqlc.narg(version)::uuid IS NULL OR done.version_id = sqlc.narg(version)::uuid
ORDER BY done.done_at, done.item_id;

-- name: LineStopsByPeople :many
-- The andon pulls people made on each of the factories in [since, until) (andon_pulls_factory_idx).
SELECT factory_id, count(*) AS stops FROM andon_pulls
WHERE factory_id = ANY(sqlc.arg(factories)::uuid[]) AND pulled_at >= sqlc.arg(since)::timestamptz
  AND pulled_at < sqlc.arg(until)::timestamptz AND pulled_by LIKE 'user:%'
GROUP BY factory_id;

-- name: TouchEvent :one
-- The item and factory of an event, to read its item's touches by item.
SELECT item_id, factory_id FROM item_events WHERE id = $1;

-- name: LabelTouch :execrows
-- Sets the reason of the touch named by event, adding the row an older event lacks; nothing when
-- event names no touch of item.
INSERT INTO item_touch_labels AS l (event_id, org_id, item_id, reason, reason_by, updated_at)
SELECT t.event_id, t.org_id, t.item_id, sqlc.arg(reason)::text, sqlc.arg(reason_by)::text, now()
FROM item_touches t
WHERE t.item_id = sqlc.arg(item_id) AND t.event_id = sqlc.arg(event_id)::bigint
ON CONFLICT (event_id) DO UPDATE SET reason = EXCLUDED.reason, reason_by = EXCLUDED.reason_by, updated_at = EXCLUDED.updated_at;
