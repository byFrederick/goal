-- Notifications are claims, never data (review 2.2 M4): any role that may connect can NOTIFY a
-- channel, so goald reads each notified row back in a read-only transaction bound to the
-- organization the payload names: listeners get the row's fields (item and andon events) or a
-- claim the row bears out (definition notices). A row the organization cannot see (it does not
-- exist, or is another organization's) answers no row.

-- name: NoticeEvent :one
-- An item event with the item's number and its state right after the event: the state of the
-- item's span covering the event's time (the latest to begin by then), or the item's own state
-- when no span covers it (done or cancelled, or an item without spans) (item_spans_item_idx).
SELECT e.factory_id, e.item_id, e.kind, e.station, e.at, w.number,
       coalesce((SELECT s.state FROM item_spans s
                 WHERE s.item_id = e.item_id AND s.entered_at <= e.at AND (s.left_at IS NULL OR s.left_at > e.at)
                 ORDER BY s.entered_at DESC, s.id DESC LIMIT 1), w.state)::text AS state
FROM item_events e
JOIN work_items w ON w.id = e.item_id
WHERE e.id = sqlc.arg(id);

-- name: NoticeConnection :one
-- The name in a connection's latest audit entry of one of actions (audit_log_target_idx): what a
-- connection event is read back from.
SELECT coalesce(data ->> 'name', '')::text AS name FROM audit_log
WHERE target_kind = 'connection' AND target_id = sqlc.arg(target_id)::text AND action = ANY (sqlc.arg(actions)::text[])
ORDER BY id DESC
LIMIT 1;

-- name: NoticeKnowledgeSource :one
-- A knowledge source's name and sync status: what a knowledge event is read back from.
SELECT name, status FROM knowledge_sources WHERE id = sqlc.arg(id);

-- name: NoticeAndon :one
SELECT factory_id, pulled_at, cleared_at FROM andon_pulls WHERE id = sqlc.arg(id);

-- name: NoticeVersion :one
SELECT factory_id, state, number, revision FROM definition_versions WHERE id = sqlc.arg(id);

-- name: NoticeCurrent :one
SELECT current_version_id FROM factories WHERE id = sqlc.arg(factory_id);
