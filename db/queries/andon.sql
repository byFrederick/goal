-- The andon cord (2.2-E §4.4): one open pull per factory (andon_pulls_open_key), every pull kept
-- as history. Row-level security limits every query to the transaction's organization; the
-- composite foreign key refuses a factory of another organization.

-- name: InsertAndonPull :one
INSERT INTO andon_pulls (factory_id, kind, station, reason, pulled_by, pulled_at, data)
VALUES (sqlc.arg(factory_id), sqlc.arg(kind), sqlc.narg(station), sqlc.arg(reason), sqlc.arg(pulled_by),
        sqlc.arg(pulled_at), sqlc.arg(data))
RETURNING *;

-- name: ClearOpenAndon :one
UPDATE andon_pulls
SET cleared_at = sqlc.arg(cleared_at), cleared_by = sqlc.arg(cleared_by), note = sqlc.narg(note)
WHERE factory_id = sqlc.arg(factory_id) AND cleared_at IS NULL
RETURNING *;

-- name: OpenAndon :one
SELECT * FROM andon_pulls WHERE factory_id = $1 AND cleared_at IS NULL;

-- name: AndonPulls :many
-- A factory's pulls, the most recently pulled first (andon_pulls_factory_idx).
SELECT * FROM andon_pulls
WHERE factory_id = sqlc.arg(factory_id)
ORDER BY pulled_at DESC, id DESC
LIMIT sqlc.arg(max_pulls)::bigint;

-- name: GateVerdicts :many
-- Of a gate station's last n verdicts (item_events_verdicts_idx), those after `after`, newest
-- first.
SELECT kind, at, coalesce(data->>'category', '')::text AS category, coalesce(data->>'reason', '')::text AS reason
FROM (
  SELECT kind, at, data, id FROM item_events
  WHERE factory_id = sqlc.arg(factory_id) AND station = sqlc.arg(station) AND kind IN ('gate_approved', 'gate_rejected')
  ORDER BY id DESC
  LIMIT sqlc.arg(max_verdicts)::bigint
) last
WHERE at > sqlc.arg(after)
ORDER BY id DESC;

-- name: AndonFactory :one
-- The factory a pull is for, when the transaction's organization has it: andon_pulls_open_key
-- spans organizations, so this is checked before the insert can tell another organization's
-- open pull apart.
SELECT id FROM factories WHERE id = $1;
