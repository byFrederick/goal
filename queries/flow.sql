-- Reads of the flow read model (ports.FlowStats): spans of time and the statistics derived from
-- them. Every query runs under the transaction's organization (row-level security).

-- name: ItemSpans :many
-- An item's spans in the order it entered them; the trigger keeps them contiguous.
SELECT station, state, rework_round, rework_by, entered_at, left_at
FROM item_spans
WHERE item_id = $1
ORDER BY entered_at, id;

-- name: CurrentRun :one
-- The item's newest running run under its live lease, with the lease's slot and the usage events
-- the run reported so far (item_events_run_id_idx).
SELECT r.*, l.slot AS lease_slot,
       (SELECT count(*) FROM item_events e WHERE e.run_id = r.id AND e.kind = sqlc.arg(usage_kind)::text) AS usage_turns
FROM agent_runs r
JOIN leases l ON l.item_id = r.item_id
WHERE r.item_id = sqlc.arg(item_id) AND r.status = 'running'
ORDER BY r.started_at DESC, r.id DESC
LIMIT 1;
