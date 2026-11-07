-- The Floor's reports over a window (ports.FlowStats): the lead-time bridge's item times and the
-- constraint report's station waits. Every query runs under the transaction's organization
-- (row-level security).

-- name: FlowItemTimes :many
-- The last max_items items of the type done in [since, until), each with its lead time in
-- seconds (from started_at, or created_at when it never started) and its closed spans since then
-- summed in seconds by station, state and rework_by (one row per item without spans), by when it was done
-- (work_items_done_at_idx, then item_spans_item_idx). ci_seconds is the part of those seconds
-- that first-round queued or ready spans spent waiting for CI at their station: their overlap with
-- the union of the item's CI waits there, an open wait having no end (ci_waits_org_item_idx;
-- ADR 0044 decision 10).
WITH done AS (
  SELECT w.id, coalesce(w.started_at, w.created_at) AS started_at, w.done_at FROM work_items w
  WHERE w.factory_id = sqlc.arg(factory_id) AND w.done_at >= sqlc.arg(since)::timestamptz
    AND w.done_at < sqlc.arg(until)::timestamptz
    AND (sqlc.arg(type)::text = '' OR w.type = sqlc.arg(type)::text)
  ORDER BY w.done_at DESC, w.id DESC
  LIMIT sqlc.arg(max_items)
),
ci AS (
  SELECT c.item_id, c.station, range_agg(tstzrange(c.started_at, c.ended_at)) AS waits
  FROM done d JOIN ci_waits c ON c.org_id = goal_org() AND c.item_id = d.id
  GROUP BY c.item_id, c.station
)
SELECT d.id AS item_id,
       extract(epoch FROM d.done_at - d.started_at)::float8 AS lead_seconds,
       s.station, s.state, s.rework_by,
       coalesce(sum(extract(epoch FROM s.left_at - s.entered_at)), 0)::float8 AS seconds,
       coalesce(sum((SELECT sum(extract(epoch FROM upper(r) - lower(r)))
                     FROM unnest(ci.waits * tstzmultirange(tstzrange(s.entered_at, s.left_at))) r)), 0)::float8 AS ci_seconds
FROM done d
LEFT JOIN item_spans s ON s.item_id = d.id AND s.left_at IS NOT NULL AND s.entered_at >= d.started_at
LEFT JOIN ci ON ci.item_id = d.id AND ci.station = s.station AND s.state IN ('queued', 'ready') AND s.rework_by IS NULL
GROUP BY d.id, d.done_at, d.started_at, s.station, s.state, s.rework_by
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
