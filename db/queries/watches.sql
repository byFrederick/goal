-- Watches (00100, ADR 0039 decisions 14 and 15), under row-level security: every statement sees
-- the transaction's organization only.

-- name: StationOutcomes :many
-- Each item's outcome at the station on each of versions: its runs there on one version, from
-- the first (at) until its first run there on another version (until), counted once none of
-- them runs and the item left the station since. sent_back says a person sent it back at the
-- station in that time; cost_usd sums those runs; lead_seconds sums the station's spans that
-- left in that time. The runs come from agent_runs_version_idx, the items' other runs from
-- agent_runs_item_id_idx.
WITH items AS (
  SELECT DISTINCT a.item_id FROM agent_runs a
  WHERE a.definition_version_id = ANY(sqlc.arg(versions)::uuid[]) AND a.factory_id = sqlc.arg(factory_id)
    AND a.station = sqlc.arg(station)::text AND a.kind = 'station' AND a.item_id IS NOT NULL
), groups AS (
  SELECT r.item_id, r.definition_version_id AS version_id, min(r.started_at) AS first_at, sum(r.cost_usd) AS cost,
         bool_or(r.status = 'running') AS running
  FROM agent_runs r JOIN items i ON i.item_id = r.item_id
  WHERE r.factory_id = sqlc.arg(factory_id) AND r.station = sqlc.arg(station)::text AND r.kind = 'station'
    AND r.definition_version_id IS NOT NULL
  GROUP BY r.item_id, r.definition_version_id
), spans AS (
  SELECT g.*, lead(g.first_at) OVER (PARTITION BY g.item_id ORDER BY g.first_at) AS until FROM groups g
)
SELECT s.version_id::uuid AS version_id, s.item_id::uuid AS item_id, s.first_at::timestamptz AS at, s.cost::float8 AS cost_usd,
       EXISTS (
         SELECT 1 FROM item_touches t
         WHERE t.item_id = s.item_id AND t.factory_id = sqlc.arg(factory_id) AND t.station = sqlc.arg(station)::text
           AND t.kind IN ('rejected', 'gate_rejected') AND t.at >= s.first_at AND (s.until IS NULL OR t.at < s.until)
       ) AS sent_back,
       coalesce((
         SELECT sum(extract(epoch FROM p.left_at - p.entered_at)) FROM item_spans p
         WHERE p.item_id = s.item_id AND p.factory_id = sqlc.arg(factory_id) AND p.station = sqlc.arg(station)::text
           AND p.left_at >= s.first_at AND (s.until IS NULL OR p.left_at < s.until)
       ), 0)::float8 AS lead_seconds
FROM spans s
WHERE s.version_id = ANY(sqlc.arg(versions)::uuid[]) AND NOT s.running
  AND EXISTS (
    SELECT 1 FROM item_spans p
    WHERE p.item_id = s.item_id AND p.factory_id = sqlc.arg(factory_id) AND p.station = sqlc.arg(station)::text
      AND p.left_at >= s.first_at)
ORDER BY s.first_at, s.item_id;

-- name: UpsertWatch :one
-- Writes an open watch; a closed one is left as it is (no row).
INSERT INTO watches (id, factory_id, version_id, proposal_id, station, metric, before_n, before_value, after_n, after_value,
                     interval_low, interval_high, verdict, opened_at, closed_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(version_id), sqlc.narg(proposal_id), sqlc.arg(station), sqlc.arg(metric),
        sqlc.arg(before_n), sqlc.arg(before_value), sqlc.arg(after_n), sqlc.arg(after_value), sqlc.narg(interval_low),
        sqlc.narg(interval_high), sqlc.arg(verdict), sqlc.arg(opened_at), sqlc.narg(closed_at), sqlc.arg(updated_at))
ON CONFLICT (org_id, factory_id, version_id, station, metric) DO UPDATE
SET before_n = excluded.before_n, before_value = excluded.before_value, after_n = excluded.after_n,
    after_value = excluded.after_value, interval_low = excluded.interval_low, interval_high = excluded.interval_high,
    verdict = excluded.verdict, closed_at = excluded.closed_at, updated_at = excluded.updated_at
WHERE watches.closed_at IS NULL
RETURNING *;

-- name: VersionWatches :many
-- The watches of the factory's versions, by version, station and metric.
SELECT * FROM watches
WHERE factory_id = sqlc.arg(factory_id) AND version_id = ANY(sqlc.arg(versions)::uuid[])
ORDER BY version_id, station, metric;

-- name: ProposalWatches :many
SELECT * FROM watches WHERE proposal_id = ANY(sqlc.arg(proposals)::uuid[]) ORDER BY proposal_id, station, metric;

-- name: WorseProposalWatches :many
-- The organization's watches of a proposal's own number whose verdict is worse, with the
-- proposal's number, unless an undo of the proposal is proposed, published or applied.
SELECT w.*, p.number AS proposal_number
FROM watches w
JOIN jonah_proposals p ON p.id = w.proposal_id
WHERE w.verdict = 'worse'
  AND NOT EXISTS (
    SELECT 1 FROM jonah_proposals u
    WHERE u.undoes = w.proposal_id AND u.state IN ('proposed', 'published', 'applied'))
ORDER BY w.updated_at DESC, w.id
LIMIT sqlc.arg(max_watches);
