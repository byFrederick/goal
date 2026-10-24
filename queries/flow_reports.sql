-- The Floor's reports over a window (ports.FlowStats): the lead-time bridge's item times and the
-- constraint report's station waits. Every query runs under the transaction's organization
-- (row-level security).

-- name: FlowItemTimes :many
-- Each item of the type done in [since, until) with its lead time in seconds and its closed
-- spans' seconds summed by station, state and rework_by (one row per item without spans), by
-- when it was done (work_items_done_at_idx, then item_spans_item_idx).
SELECT w.id AS item_id,
       extract(epoch FROM w.done_at - w.created_at)::float8 AS lead_seconds,
       s.station, s.state, s.rework_by,
       coalesce(sum(extract(epoch FROM s.left_at - s.entered_at)), 0)::float8 AS seconds
FROM work_items w
LEFT JOIN item_spans s ON s.item_id = w.id AND s.left_at IS NOT NULL
WHERE w.factory_id = sqlc.arg(factory_id) AND w.done_at >= sqlc.arg(since)::timestamptz
  AND w.done_at < sqlc.arg(until)::timestamptz
  AND (sqlc.arg(type)::text = '' OR w.type = sqlc.arg(type)::text)
GROUP BY w.id, w.done_at, w.created_at, s.station, s.state, s.rework_by
ORDER BY w.done_at, w.id;
