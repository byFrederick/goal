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

-- name: LiveRuns :many
-- The usage events of each item's newest running run under its live lease, the data of its
-- newest tool call and of its newest call of the todo tool, for the board's cards (leases_pkey
-- on item_id, item_events_run_id_idx).
SELECT DISTINCT ON (r.item_id) r.item_id,
       (SELECT count(*) FROM item_events e WHERE e.run_id = r.id AND e.kind = sqlc.arg(usage_kind)::text) AS usage_turns,
       (SELECT e.data FROM item_events e WHERE e.run_id = r.id AND e.kind = sqlc.arg(tool_call_kind)::text
        ORDER BY e.id DESC LIMIT 1)::jsonb AS last_tool_call,
       (SELECT e.data FROM item_events e WHERE e.run_id = r.id AND e.kind = sqlc.arg(tool_call_kind)::text
          AND e.data->>'tool' = sqlc.arg(todo_tool)::text
        ORDER BY e.id DESC LIMIT 1)::jsonb AS last_plan
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

-- name: StationSpend :many
-- The cost of each station's runs started in [since, until), failed and retried ones included, on
-- agent_runs_factory_started_idx.
SELECT station, coalesce(sum(cost_usd), 0)::float8 AS spend_usd
FROM agent_runs
WHERE factory_id = sqlc.arg(factory_id) AND started_at >= sqlc.arg(since)::timestamptz AND started_at < sqlc.arg(until)::timestamptz
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

-- name: FlowAssemblyStats :one
-- Assemblies (items with parts) done, and their lead-time (created to last part done)
-- percentiles in seconds, interpolated between ranks: done items on work_items_done_at_idx,
-- their parts on work_items_parent_part_key_idx. Cancelled assemblies never reach done.
SELECT count(*) AS assemblies_done,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM w.done_at - w.created_at)::float8), 0)::float8 AS p50_seconds,
       coalesce(percentile_cont(0.85) WITHIN GROUP (ORDER BY extract(epoch FROM w.done_at - w.created_at)::float8), 0)::float8 AS p85_seconds
FROM work_items w
WHERE w.factory_id = sqlc.arg(factory_id) AND w.done_at >= sqlc.arg(since)::timestamptz AND w.done_at < sqlc.arg(until)::timestamptz
  AND w.state = 'done' AND (sqlc.arg(type)::text = '' OR w.type = sqlc.arg(type)::text)
  AND EXISTS (SELECT 1 FROM work_items p WHERE p.parent_id = w.id AND p.part_key IS NOT NULL);

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

-- name: AgentLeases :one
-- The agent's lease spans that ended in [since, until) with a finished outcome: how many, how many
-- advanced, and the 85th percentile of their durations in seconds (lease_spans_agent_idx).
SELECT count(*) AS finished,
       count(*) FILTER (WHERE outcome = 'advanced') AS advanced,
       coalesce(percentile_cont(0.85) WITHIN GROUP (ORDER BY extract(epoch FROM ended_at - acquired_at)), 0)::float8 AS p85_seconds
FROM lease_spans
WHERE factory_id = sqlc.arg(factory_id) AND agent = sqlc.arg(agent)::text
  AND ended_at >= sqlc.arg(since)::timestamptz AND ended_at < sqlc.arg(until)::timestamptz
  AND outcome IN ('advanced', 'rejected', 'escalated');

-- name: AgentRuns :one
-- The agent's runs started in [since, until): their cost, how many succeeded and the median turns
-- of those (agent_runs_factory_started_idx).
SELECT coalesce(sum(cost_usd), 0)::float8 AS spend_usd,
       count(*) FILTER (WHERE status = 'succeeded') AS succeeded,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY turns) FILTER (WHERE status = 'succeeded'), 0)::float8 AS median_turns
FROM agent_runs
WHERE factory_id = sqlc.arg(factory_id) AND agent = sqlc.arg(agent)::text
  AND started_at >= sqlc.arg(since)::timestamptz AND started_at < sqlc.arg(until)::timestamptz;

-- name: AgentSentBack :many
-- The agent's advanced lease spans that ended in [since, until) whose item then entered its next
-- rework round from another station, one row each with what that station said: the category and
-- reason of its rejection, else the reason of a person's move back (lease_spans_agent_idx,
-- item_spans_item_idx, item_events_rejections_idx).
SELECT coalesce(rej.category, '')::text AS category, coalesce(rej.reason, mv.reason, '')::text AS reason
FROM lease_spans l
JOIN LATERAL (
  SELECT s.entered_at, s.rework_by FROM item_spans s
  WHERE s.item_id = l.item_id AND s.rework_round = l.rework_round + 1 AND s.rework_by <> l.station
  ORDER BY s.entered_at, s.id
  LIMIT 1
) back ON true
LEFT JOIN LATERAL (
  SELECT e.data->>'category' AS category, e.data->>'reason' AS reason FROM item_events e
  WHERE e.item_id = l.item_id AND e.kind IN ('gate_rejected', 'rejected') AND e.station = back.rework_by
    AND e.at <= back.entered_at
  ORDER BY e.at DESC, e.id DESC
  LIMIT 1
) rej ON true
LEFT JOIN LATERAL (
  SELECT e.data->>'reason' AS reason FROM item_events e
  WHERE coalesce(rej.reason, rej.category) IS NULL AND e.item_id = l.item_id AND e.kind = 'moved'
    AND e.data->>'manual' = 'true' AND e.station = back.rework_by AND e.at <= back.entered_at
  ORDER BY e.at DESC, e.id DESC
  LIMIT 1
) mv ON true
WHERE l.factory_id = sqlc.arg(factory_id) AND l.agent = sqlc.arg(agent)::text AND l.outcome = 'advanced'
  AND l.ended_at >= sqlc.arg(since)::timestamptz AND l.ended_at < sqlc.arg(until)::timestamptz;

-- name: AgentDeniedTools :many
-- The max_tools tools the agent's runs started in [since, until) were most often denied, most
-- denied first (ties by name, byte order as domain.Tallies), each with its calls and all_calls,
-- every denied call counted: tool_denied events by their tool, gateway calls with outcome denied
-- as <connection>__<tool> (agent_runs_factory_started_idx, item_events_run_id_idx). Calls without
-- a tool name are not counted.
SELECT tool, calls, (sum(calls) OVER ())::bigint AS all_calls
FROM (
  SELECT (CASE WHEN e.kind = 'tool_denied' THEN coalesce(e.data->>'tool', '')
               ELSE coalesce(e.data->>'connection', '') || '__' || coalesce(e.data->>'tool', '') END)::text AS tool,
         count(*) AS calls
  FROM agent_runs r
  JOIN item_events e ON e.run_id = r.id
  WHERE r.factory_id = sqlc.arg(factory_id) AND r.agent = sqlc.arg(agent)::text
    AND r.started_at >= sqlc.arg(since)::timestamptz AND r.started_at < sqlc.arg(until)::timestamptz
    AND (e.kind = 'tool_denied' OR (e.kind = 'gateway_tool_call' AND e.data->>'outcome' = 'denied'))
  GROUP BY 1
) denied
WHERE tool <> ''
ORDER BY calls DESC, tool COLLATE "C"
LIMIT sqlc.arg(max_tools);
