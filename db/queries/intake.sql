-- Intake triggers' state (ADR 0026, 00071): webhook endpoints, their deliveries and the trigger
-- firing log. Every query runs as goal_app under row-level security, except the two definers'
-- callers and goal db rekey's (as goal_owner).

-- name: InsertWebhookEndpoint :one
-- org_id comes from its default, goal_org().
INSERT INTO webhook_endpoints (id, factory_id, name, kind, key_id, nonce, ciphertext)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(name), sqlc.arg(kind), sqlc.arg(key_id), sqlc.arg(nonce), sqlc.arg(ciphertext))
RETURNING created_at;

-- name: WebhookEndpoints :many
SELECT id, factory_id, name, kind, created_at, rotated_at FROM webhook_endpoints
WHERE factory_id = sqlc.arg(factory_id)
ORDER BY name;

-- name: WebhookEndpoint :one
SELECT id, factory_id, name, kind, created_at, rotated_at FROM webhook_endpoints WHERE id = sqlc.arg(id);

-- name: SealedWebhookSecret :one
SELECT key_id, nonce, ciphertext FROM webhook_endpoints WHERE id = sqlc.arg(id);

-- name: RotateWebhookSecret :one
UPDATE webhook_endpoints SET key_id = sqlc.arg(key_id), nonce = sqlc.arg(nonce), ciphertext = sqlc.arg(ciphertext), rotated_at = now()
WHERE id = sqlc.arg(id)
RETURNING id, factory_id, name, kind, created_at, rotated_at;

-- name: DeleteWebhookEndpoint :execrows
DELETE FROM webhook_endpoints WHERE id = sqlc.arg(id);

-- name: WebhookEndpointOrg :one
-- Any organization's endpoint, through the definer; no row for none.
SELECT o.org::uuid AS org_id FROM goal_webhook_endpoint_org(sqlc.arg(id)::uuid) AS o (org) WHERE o.org IS NOT NULL;

-- name: StaleSealedWebhookSecrets :many
-- goal db rekey, as goal_owner: a batch of endpoints sealed under another key than the active one,
-- in every organization, locked.
SELECT id, org_id, key_id, nonce, ciphertext FROM webhook_endpoints
WHERE key_id <> sqlc.arg(active)::text
ORDER BY id
LIMIT sqlc.arg(batch)
FOR NO KEY UPDATE;

-- name: ResealWebhookSecret :exec
-- goal db rekey: the same secret under the active key; rotated_at stays, as the secret does.
UPDATE webhook_endpoints SET key_id = sqlc.arg(key_id), nonce = sqlc.arg(nonce), ciphertext = sqlc.arg(ciphertext)
WHERE id = sqlc.arg(id);

-- name: InsertWebhookDelivery :exec
INSERT INTO webhook_deliveries (id, endpoint_id, received_at, headers, body, outcome, reason, item_id)
VALUES (sqlc.arg(id), sqlc.arg(endpoint_id), sqlc.arg(received_at), sqlc.arg(headers), sqlc.arg(body), sqlc.arg(outcome),
        sqlc.arg(reason), sqlc.narg(item_id));

-- name: WebhookDeliveries :many
-- An endpoint's deliveries newest first, after the cursor (received_at, id); a zero cursor, an
-- id of all zeros at the latest instant, starts at the newest.
SELECT id, endpoint_id, received_at, headers, body, outcome, reason, item_id FROM webhook_deliveries
WHERE endpoint_id = sqlc.arg(endpoint_id)
  AND (sqlc.narg(after_at)::timestamptz IS NULL OR (received_at, id) < (sqlc.narg(after_at)::timestamptz, sqlc.arg(after_id)::uuid))
ORDER BY received_at DESC, id DESC
LIMIT sqlc.arg(max_rows);

-- name: WebhookDelivery :one
SELECT id, endpoint_id, received_at, headers, body, outcome, reason, item_id FROM webhook_deliveries
WHERE endpoint_id = sqlc.arg(endpoint_id) AND id = sqlc.arg(id);

-- name: WebhookDeliveryDays :many
-- The factory's endpoints' deliveries since the instant, per endpoint and UTC day; each endpoint's
-- rows come from webhook_deliveries_endpoint_idx (endpoint_id, received_at).
SELECT d.endpoint_id, date_trunc('day', d.received_at, 'UTC')::timestamptz AS day,
       count(*) FILTER (WHERE d.outcome <> 'refused') AS delivered,
       count(*) FILTER (WHERE d.outcome = 'refused') AS refused
FROM webhook_endpoints e
JOIN webhook_deliveries d ON d.endpoint_id = e.id AND d.received_at >= sqlc.arg(since)
WHERE e.factory_id = sqlc.arg(factory_id)
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: PruneWebhookDeliveries :one
-- Every organization's deliveries received before the instant, through the definer.
SELECT goal_prune_webhook_deliveries(sqlc.arg(before)::timestamptz)::bigint AS deleted;

-- name: InsertTriggerFiring :one
-- Returns nothing (no rows) when the trigger already has a firing with this key.
INSERT INTO trigger_firings (id, factory_id, trigger, source, event_key, idempotency_key, dedupe, outcome, reason, item_id, at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(trigger), sqlc.arg(source), sqlc.arg(event_key), sqlc.arg(idempotency_key),
        sqlc.arg(dedupe), sqlc.arg(outcome), sqlc.arg(reason), sqlc.narg(item_id), sqlc.arg(at))
ON CONFLICT (org_id, factory_id, trigger, idempotency_key) DO NOTHING
RETURNING id;

-- name: TriggerFiringByKey :one
SELECT id, factory_id, trigger, source, event_key, idempotency_key, dedupe, outcome, reason, item_id, at FROM trigger_firings
WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND idempotency_key = sqlc.arg(idempotency_key);

-- name: TriggerFailureStreak :one
-- The trigger's firings with the dedupe value after its newest one with that value that created
-- an item (all of them when none did), for only.failuresInARow.
WITH last_created AS (
  SELECT at, id FROM trigger_firings
  WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND dedupe = sqlc.arg(dedupe)::text AND outcome = 'created'
  ORDER BY at DESC, id DESC
  LIMIT 1
)
SELECT count(*) FROM trigger_firings f
WHERE f.factory_id = sqlc.arg(factory_id) AND f.trigger = sqlc.arg(trigger) AND f.dedupe = sqlc.arg(dedupe)::text
  AND NOT EXISTS (SELECT 1 FROM last_created c WHERE (f.at, f.id) <= (c.at, c.id));

-- name: TriggerCreatedSince :one
SELECT count(*) FROM trigger_firings
WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND outcome = 'created' AND at >= sqlc.arg(since);

-- name: OpenItemByDedupe :one
SELECT f.item_id::uuid AS item_id FROM trigger_firings f
JOIN work_items w ON w.id = f.item_id
WHERE f.factory_id = sqlc.arg(factory_id) AND f.trigger = sqlc.arg(trigger) AND f.outcome = 'created'
  AND f.dedupe = sqlc.arg(value)::text AND sqlc.arg(value)::text <> '' AND w.state NOT IN ('done', 'cancelled')
ORDER BY f.at DESC, f.id DESC
LIMIT 1;

-- name: ItemSourceTally :many
SELECT coalesce(source, '')::text AS source, count(*) AS items FROM work_items
WHERE factory_id = sqlc.arg(factory_id) AND created_at >= sqlc.arg(since)
GROUP BY 1
ORDER BY 1;

-- name: TriggerStats :many
SELECT w.trigger::text AS trigger, count(*) AS created,
       count(*) FILTER (WHERE EXISTS (SELECT 1 FROM item_events e WHERE e.item_id = w.id AND e.kind = 'merged')) AS merged,
       coalesce(sum(w.cost_usd), 0)::numeric AS spend_usd
FROM work_items w
WHERE w.factory_id = sqlc.arg(factory_id) AND w.trigger IS NOT NULL AND w.created_at >= sqlc.arg(since)
GROUP BY w.trigger
ORDER BY w.trigger;

-- name: LastItemSources :many
-- The factory's newest items (created_at, then id) by source, with each source's first and last
-- instants.
WITH last AS (
  SELECT source, created_at FROM work_items
  WHERE factory_id = sqlc.arg(factory_id)
  ORDER BY created_at DESC, id DESC
  LIMIT sqlc.arg(max_items)
)
SELECT coalesce(source, '')::text AS source, count(*) AS items, min(created_at)::timestamptz AS first_at,
       max(created_at)::timestamptz AS last_at
FROM last
GROUP BY 1
ORDER BY 1;

-- name: TriggerDays :many
-- The factory's firings that created an item, per trigger and UTC day.
SELECT trigger, (date_trunc('day', at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')::timestamptz AS day, count(*) AS created
FROM trigger_firings
WHERE factory_id = sqlc.arg(factory_id) AND outcome = 'created' AND at >= sqlc.arg(since)
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: TriggerFirings :many
-- The trigger's firings newest first, with their items' numbers (0 for none) and merged events.
SELECT f.id, f.factory_id, f.trigger, f.source, f.event_key, f.idempotency_key, f.dedupe, f.outcome, f.reason, f.item_id, f.at,
       coalesce(w.number, 0)::bigint AS item_number,
       EXISTS (SELECT 1 FROM item_events e WHERE e.item_id = f.item_id AND e.kind = 'merged') AS merged
FROM trigger_firings f
LEFT JOIN work_items w ON w.id = f.item_id
WHERE f.factory_id = sqlc.arg(factory_id) AND f.trigger = sqlc.arg(trigger)
ORDER BY f.at DESC, f.id DESC
LIMIT sqlc.arg(max_rows);
