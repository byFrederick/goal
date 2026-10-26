-- The Floor's reports over a window (ports.FlowStats): the lead-time bridge's item times and the
-- constraint report's station waits. Every query runs under the transaction's organization
-- (row-level security).

-- name: FlowItemTimes :many
-- The last max_items items of the type done in [since, until), each with its lead time in
-- seconds and its closed spans' seconds summed by station, state and rework_by (one row per
-- item without spans), by when it was done (work_items_done_at_idx, then item_spans_item_idx).
WITH done AS (
  SELECT w.id, w.created_at, w.done_at FROM work_items w
  WHERE w.factory_id = sqlc.arg(factory_id) AND w.done_at >= sqlc.arg(since)::timestamptz
    AND w.done_at < sqlc.arg(until)::timestamptz
    AND (sqlc.arg(type)::text = '' OR w.type = sqlc.arg(type)::text)
  ORDER BY w.done_at DESC, w.id DESC
  LIMIT sqlc.arg(max_items)
)
SELECT d.id AS item_id,
       extract(epoch FROM d.done_at - d.created_at)::float8 AS lead_seconds,
       s.station, s.state, s.rework_by,
       coalesce(sum(extract(epoch FROM s.left_at - s.entered_at)), 0)::float8 AS seconds
FROM done d
LEFT JOIN item_spans s ON s.item_id = d.id AND s.left_at IS NOT NULL
GROUP BY d.id, d.done_at, d.created_at, s.station, s.state, s.rework_by
ORDER BY d.done_at, d.id;

-- name: StationWaitTimes :many
-- How long items waited (queued or ready) at each station in [since, until): the waiting spans
-- that overlap the window, each clipped to it, summed, and the items that had one
-- (item_spans_window_idx).
SELECT station, count(DISTINCT item_id) AS waiters,
       coalesce(sum(extract(epoch FROM least(coalesce(left_at, sqlc.arg(until)::timestamptz), sqlc.arg(until)::timestamptz)
                                 - greatest(entered_at, sqlc.arg(since)::timestamptz))), 0)::float8 AS waiting_seconds
FROM item_spans
WHERE factory_id = sqlc.arg(factory_id) AND state IN ('queued', 'ready')
  AND (left_at IS NULL OR left_at > sqlc.arg(since)::timestamptz) AND entered_at < sqlc.arg(until)::timestamptz
GROUP BY station;
