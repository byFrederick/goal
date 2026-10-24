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

-- name: LiveTurns :many
-- The usage events of each item's newest running run under its live lease, for the board's
-- cards (leases_pkey on item_id, item_events_run_id_idx).
SELECT DISTINCT ON (r.item_id) r.item_id,
       (SELECT count(*) FROM item_events e WHERE e.run_id = r.id AND e.kind = sqlc.arg(usage_kind)::text) AS usage_turns
FROM leases l
JOIN agent_runs r ON r.item_id = l.item_id
WHERE l.factory_id = sqlc.arg(factory_id) AND r.status = 'running'
ORDER BY r.item_id, r.started_at DESC, r.id DESC;

-- name: StationLeaseTimes :many
-- How long each station's slots held leases in [since, until): the lease spans and live leases
-- that overlap the window, each clipped to it (lease_spans_station_idx).
SELECT station, count(*) AS leases,
       coalesce(sum(extract(epoch FROM least(ended_at, sqlc.arg(until)::timestamptz)
                                - greatest(acquired_at, sqlc.arg(since)::timestamptz))), 0)::float8 AS busy_seconds
FROM (
  SELECT s.station, s.acquired_at, s.ended_at FROM lease_spans s
  WHERE s.factory_id = sqlc.arg(factory_id)::uuid AND s.ended_at > sqlc.arg(since)::timestamptz
    AND s.acquired_at < sqlc.arg(until)::timestamptz
  UNION ALL
  SELECT l.station, l.acquired_at, sqlc.arg(until)::timestamptz FROM leases l
  WHERE l.factory_id = sqlc.arg(factory_id)::uuid AND l.acquired_at < sqlc.arg(until)::timestamptz
) held
GROUP BY station;

-- name: LeaseHistoryStart :one
-- The earliest acquisition of the factory's lease spans and live leases: where its busy shares
-- begin to mean anything. No row when the factory never held a lease.
SELECT acquired_at FROM (
  (SELECT s.acquired_at FROM lease_spans s WHERE s.factory_id = sqlc.arg(factory_id)::uuid ORDER BY s.acquired_at LIMIT 1)
  UNION ALL
  (SELECT l.acquired_at FROM leases l WHERE l.factory_id = sqlc.arg(factory_id)::uuid ORDER BY l.acquired_at LIMIT 1)
) earliest
ORDER BY acquired_at
LIMIT 1;

-- name: StationWaiting :many
-- Items waiting (queued or ready) at each station at until and 24 h before it (day_ago), and when
-- the oldest of those waiting at until began, from the spans open at those instants
-- (item_spans_window_idx).
SELECT station,
       count(*) FILTER (WHERE entered_at <= sqlc.arg(until)::timestamptz
                          AND (left_at IS NULL OR left_at > sqlc.arg(until)::timestamptz)) AS waiting_now,
       count(*) FILTER (WHERE entered_at <= sqlc.arg(day_ago)::timestamptz
                          AND (left_at IS NULL OR left_at > sqlc.arg(day_ago)::timestamptz)) AS waiting_day_ago,
       coalesce(min(entered_at) FILTER (WHERE entered_at <= sqlc.arg(until)::timestamptz
                          AND (left_at IS NULL OR left_at > sqlc.arg(until)::timestamptz)), sqlc.arg(until)::timestamptz)::timestamptz AS oldest
FROM item_spans
WHERE factory_id = sqlc.arg(factory_id) AND state IN ('queued', 'ready')
  AND (left_at IS NULL OR left_at > sqlc.arg(day_ago)::timestamptz) AND entered_at <= sqlc.arg(until)::timestamptz
GROUP BY station;

-- name: StationVerdicts :many
-- Gate verdicts given at each station in [since, until) and the rejections among them
-- (item_events_verdicts_idx).
SELECT station, count(*) AS verdicts, count(*) FILTER (WHERE kind = 'gate_rejected') AS rejected
FROM item_events
WHERE factory_id = sqlc.arg(factory_id) AND kind IN ('gate_approved', 'gate_rejected')
  AND at >= sqlc.arg(since)::timestamptz AND at < sqlc.arg(until)::timestamptz
GROUP BY station;

-- Windowed metrics (FlowStats.FlowMetrics, DoneByBucket) over [since, until). Each keeps the
-- items of one work type, every type when type is ''.

-- name: FlowDonePerDay :many
-- Items done by UTC day, on work_items_done_at_idx.
SELECT date_trunc('day', done_at, 'UTC')::timestamptz AS day, count(*) AS done
FROM work_items
WHERE factory_id = sqlc.arg(factory_id) AND done_at >= sqlc.arg(since)::timestamptz AND done_at < sqlc.arg(until)::timestamptz
  AND (sqlc.arg(type)::text = '' OR type = sqlc.arg(type)::text)
GROUP BY 1
ORDER BY 1;

-- name: FlowDoneStats :one
-- Items done, those reworked, and lead-time (created to done) percentiles in seconds,
-- interpolated between ranks.
SELECT count(*) AS items_done,
       count(*) FILTER (WHERE rework_count > 0) AS reworked,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM done_at - created_at)::float8), 0)::float8 AS p50_seconds,
       coalesce(percentile_cont(0.85) WITHIN GROUP (ORDER BY extract(epoch FROM done_at - created_at)::float8), 0)::float8 AS p85_seconds
FROM work_items
WHERE factory_id = sqlc.arg(factory_id) AND done_at >= sqlc.arg(since)::timestamptz AND done_at < sqlc.arg(until)::timestamptz
  AND (sqlc.arg(type)::text = '' OR type = sqlc.arg(type)::text);

-- name: FlowOpenItems :one
-- Items in the line now, on work_items_open_idx.
SELECT count(*) FROM work_items
WHERE factory_id = sqlc.arg(factory_id) AND state NOT IN ('done', 'cancelled')
  AND (sqlc.arg(type)::text = '' OR type = sqlc.arg(type)::text);

-- name: FlowSpend :one
-- The cost of the runs started in the window, on agent_runs_factory_started_idx; a run has its
-- item's work type.
SELECT coalesce(sum(r.cost_usd), 0)::float8 AS spend_usd
FROM agent_runs r
JOIN work_items w ON w.id = r.item_id
WHERE r.factory_id = sqlc.arg(factory_id) AND r.started_at >= sqlc.arg(since)::timestamptz AND r.started_at < sqlc.arg(until)::timestamptz
  AND (sqlc.arg(type)::text = '' OR w.type = sqlc.arg(type)::text);

-- name: FlowLongWaits :one
-- Items with a span waiting (queued or ready) at a station that overlaps the window and had
-- lasted over the threshold by its end, on item_spans_station_idx.
SELECT count(DISTINCT item_id)
FROM item_spans
WHERE factory_id = sqlc.arg(factory_id) AND station = sqlc.arg(station)::text
  AND entered_at < sqlc.arg(until)::timestamptz AND (left_at IS NULL OR left_at > sqlc.arg(since)::timestamptz)
  AND state IN ('queued', 'ready')
  AND least(coalesce(left_at, sqlc.arg(until)::timestamptz), sqlc.arg(until)::timestamptz) - entered_at
      > make_interval(secs => sqlc.arg(threshold_seconds)::float8)
  AND (sqlc.arg(type)::text = '' OR type = sqlc.arg(type)::text);

-- name: FlowDoneByBucket :many
-- Items done in consecutive buckets of bucket_seconds from start to until, by bucket (0 the
-- oldest; empty buckets left out), on work_items_done_at_idx.
SELECT floor(extract(epoch FROM done_at - sqlc.arg(start)::timestamptz) / sqlc.arg(bucket_seconds)::float8)::int AS bucket,
       count(*) AS done
FROM work_items
WHERE factory_id = sqlc.arg(factory_id) AND done_at >= sqlc.arg(start)::timestamptz AND done_at < sqlc.arg(until)::timestamptz
  AND (sqlc.arg(type)::text = '' OR type = sqlc.arg(type)::text)
GROUP BY 1
ORDER BY 1;
