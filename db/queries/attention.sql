-- What the app's chrome reads about an organization (2.2-C). Row-level security limits every
-- query to the organization of the transaction; none adds a WHERE org_id.

-- name: FactoryAttention :many
-- Every factory with its decisions, blocked items and open andon pull (at most one per factory,
-- andon_pulls_open_key). work_items_open_idx serves the join.
SELECT f.id AS factory_id,
       count(w.id) FILTER (WHERE w.state = 'waiting_human')::bigint AS waiting_human,
       count(w.id) FILTER (WHERE w.state = 'blocked')::bigint       AS blocked,
       a.id        AS andon_id,
       a.kind      AS andon_kind,
       a.station   AS andon_station,
       a.reason    AS andon_reason,
       a.pulled_by AS andon_pulled_by,
       a.pulled_at AS andon_pulled_at,
       a.data      AS andon_data
FROM factories f
LEFT JOIN work_items w ON w.factory_id = f.id AND w.state IN ('waiting_human', 'blocked')
LEFT JOIN andon_pulls a ON a.factory_id = f.id AND a.cleared_at IS NULL
GROUP BY f.id, a.id
ORDER BY f.id;

-- name: WaitingByType :many
-- The decisions of every factory, split by work type.
SELECT factory_id, type, count(*) AS waiting
FROM work_items
WHERE state = 'waiting_human'
GROUP BY factory_id, type
ORDER BY factory_id, type;

-- name: InboxItems :many
-- The open items of the given factories that may need a person (work_items_attention_idx, whose
-- predicate this is), the most recently changed first, each blocked one with its latest blocked
-- or paused event.
SELECT sqlc.embed(w), coalesce(b.kind, '')::text AS blocker_kind, coalesce(b.data, '{}')::jsonb AS blocker_data
FROM work_items w
LEFT JOIN LATERAL (
  SELECT e.kind, e.data FROM item_events e
  WHERE e.item_id = w.id AND e.kind IN ('blocked', 'paused')
  ORDER BY e.id DESC
  LIMIT 1
) b ON w.state = 'blocked'
WHERE (w.state IN ('waiting_human', 'blocked')
       OR (w.class_of_service = 'expedite' AND w.state NOT IN ('done', 'cancelled')))
  AND w.factory_id = ANY (sqlc.arg(factories)::uuid[])
ORDER BY w.updated_at DESC, w.id
LIMIT sqlc.arg(max_items)::bigint;
