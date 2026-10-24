-- What the app's chrome reads about an organization (2.2-C). Row-level security limits every
-- query to the organization of the transaction; none adds a WHERE org_id.

-- name: FactoryAttention :many
-- Every factory with its decisions, blocked items and open andon pull (at most one per factory,
-- andon_pulls_open_key). work_items_open_idx serves the join.
SELECT f.id AS factory_id,
       count(w.id) FILTER (WHERE w.state = 'waiting_human')::bigint AS waiting_human,
       count(w.id) FILTER (WHERE w.state = 'blocked')::bigint       AS blocked,
       a.reason    AS andon_reason,
       a.pulled_by AS andon_pulled_by,
       a.pulled_at AS andon_pulled_at
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

-- name: WaitingHumanItems :many
-- The decisions of the given factories, most recently changed first.
SELECT * FROM work_items
WHERE state = 'waiting_human' AND factory_id = ANY (sqlc.arg(factories)::uuid[])
ORDER BY updated_at DESC, id
LIMIT sqlc.arg(max_items)::bigint;
