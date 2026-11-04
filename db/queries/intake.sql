-- Intake triggers' state (ADR 0026, 00071): webhook endpoints, their deliveries and the trigger
-- firing log. Every query runs as goal_app under row-level security, except the two definers'
-- callers and goal db rekey's (as goal_owner).

-- name: InsertWebhookEndpoint :one
-- org_id comes from its default, goal_org().
INSERT INTO webhook_endpoints (id, factory_id, name, kind, key_id, nonce, ciphertext)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(name), sqlc.arg(kind), sqlc.arg(key_id), sqlc.arg(nonce), sqlc.arg(ciphertext))
RETURNING created_at;

-- name: WebhookEndpoints :many
-- The endpoints open to the factory: its own, and the organization's webhooks with an opening.
SELECT e.id, e.factory_id, e.name, e.kind, e.sender, e.secret_id, coalesce(s.name, '')::text AS secret_name, e.enabled, e.token_header, e.token_event_path, e.token_id_path, e.created_at, e.rotated_at
FROM webhook_endpoints e
LEFT JOIN secrets s ON s.org_id = e.org_id AND s.id = e.secret_id
WHERE e.factory_id = sqlc.arg(factory_id)
   OR EXISTS (SELECT 1 FROM webhook_openings o WHERE o.webhook_id = e.id AND o.factory_id = sqlc.arg(factory_id))
ORDER BY e.name, e.id;

-- name: WebhookEndpoint :one
SELECT e.id, e.factory_id, e.name, e.kind, e.sender, e.secret_id, coalesce(s.name, '')::text AS secret_name, e.enabled, e.token_header, e.token_event_path, e.token_id_path, e.created_at, e.rotated_at
FROM webhook_endpoints e
LEFT JOIN secrets s ON s.org_id = e.org_id AND s.id = e.secret_id
WHERE e.id = sqlc.arg(id);

-- name: OrgWebhooks :many
-- The organization's webhooks (no factory), by name.
SELECT e.id, e.factory_id, e.name, e.kind, e.sender, e.secret_id, coalesce(s.name, '')::text AS secret_name, e.enabled, e.token_header, e.token_event_path, e.token_id_path, e.created_at, e.rotated_at
FROM webhook_endpoints e
LEFT JOIN secrets s ON s.org_id = e.org_id AND s.id = e.secret_id
WHERE e.factory_id IS NULL
ORDER BY e.name;

-- name: OrgWebhookByName :one
SELECT e.id, e.factory_id, e.name, e.kind, e.sender, e.secret_id, coalesce(s.name, '')::text AS secret_name, e.enabled, e.token_header, e.token_event_path, e.token_id_path, e.created_at, e.rotated_at
FROM webhook_endpoints e
LEFT JOIN secrets s ON s.org_id = e.org_id AND s.id = e.secret_id
WHERE e.factory_id IS NULL AND e.name = sqlc.arg(name);

-- name: LockOrgWebhook :one
-- The organization's webhook, locked for a patch.
SELECT id, name FROM webhook_endpoints WHERE id = sqlc.arg(id) AND factory_id IS NULL FOR UPDATE;

-- name: WebhookNameTaken :one
-- Whether an endpoint of the organization other than the one given has the name: a factory's
-- endpoint keeps its name until the conversion, so the partial unique index does not see it.
SELECT EXISTS (SELECT 1 FROM webhook_endpoints WHERE name = sqlc.arg(name) AND id <> sqlc.arg(except_id)::uuid)::bool;

-- name: InsertOrgWebhook :one
-- org_id comes from its default, goal_org(); secret_id is the secret named, none for no such one.
INSERT INTO webhook_endpoints (id, name, sender, secret_id, token_header, token_event_path, token_id_path)
SELECT sqlc.arg(id), sqlc.arg(name), sqlc.arg(sender), s.id, sqlc.narg(token_header), sqlc.narg(token_event_path), sqlc.narg(token_id_path)
FROM secrets s WHERE s.name = sqlc.arg(secret)
RETURNING secret_id, created_at;

-- name: UpdateOrgWebhook :exec
UPDATE webhook_endpoints SET name = sqlc.arg(name), enabled = sqlc.arg(enabled) WHERE id = sqlc.arg(id);

-- name: WebhookOpenings :many
-- The openings of the webhooks given, by webhook then factory.
SELECT webhook_id, factory_id, repositories FROM webhook_openings
WHERE webhook_id = ANY (sqlc.arg(webhook_ids)::uuid[])
ORDER BY webhook_id, factory_id;

-- name: UpsertWebhookOpening :exec
INSERT INTO webhook_openings (webhook_id, factory_id, repositories)
VALUES (sqlc.arg(webhook_id), sqlc.arg(factory_id), sqlc.narg(repositories)::text[])
ON CONFLICT (org_id, webhook_id, factory_id) DO UPDATE SET repositories = excluded.repositories;

-- name: DeleteWebhookOpening :exec
DELETE FROM webhook_openings WHERE webhook_id = sqlc.arg(webhook_id) AND factory_id = sqlc.arg(factory_id);

-- name: WebhookSecretUses :many
-- The secrets the organization's webhooks are signed by, by secret then webhook.
SELECT s.name AS secret, e.name AS webhook
FROM webhook_endpoints e
JOIN secrets s ON s.org_id = e.org_id AND s.id = e.secret_id
ORDER BY s.name, e.name;

-- name: SealedWebhookSecret :one
-- A factory endpoint's sealed secret; the organization's webhooks have none.
SELECT key_id::text AS key_id, nonce::bytea AS nonce, ciphertext::bytea AS ciphertext FROM webhook_endpoints
WHERE id = sqlc.arg(id) AND key_id IS NOT NULL;

-- name: NextFactoryEndpoint :one
-- The conversion (ADR 0040 decision 15): the oldest endpoint that still has a factory, locked,
-- with its sealed secret; none when every one is converted.
SELECT id, factory_id::uuid AS factory_id, name, coalesce(kind, '')::text AS kind, key_id::text AS key_id,
       nonce::bytea AS nonce, ciphertext::bytea AS ciphertext
FROM webhook_endpoints
WHERE factory_id IS NOT NULL
ORDER BY id
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: ConvertedNameTaken :one
-- Whether the conversion of endpoint id must not take the name: the organization's webhook has
-- it, or an older factory endpoint does, which keeps it when its turn comes.
SELECT EXISTS (SELECT 1 FROM webhook_endpoints
               WHERE name = sqlc.arg(name) AND id <> sqlc.arg(id) AND (factory_id IS NULL OR id < sqlc.arg(id)))::bool;

-- name: ConvertWebhookEndpoint :exec
-- The endpoint becomes the organization's webhook, signed by secret_id; its sealed columns stay.
UPDATE webhook_endpoints SET factory_id = NULL, name = sqlc.arg(name), sender = sqlc.arg(sender), secret_id = sqlc.arg(secret_id)
WHERE id = sqlc.arg(id);

-- name: RotateWebhookSecret :one
UPDATE webhook_endpoints SET key_id = sqlc.arg(key_id), nonce = sqlc.arg(nonce), ciphertext = sqlc.arg(ciphertext), rotated_at = now()
WHERE id = sqlc.arg(id) AND factory_id IS NOT NULL
RETURNING id;

-- name: DeleteWebhookEndpoint :execrows
DELETE FROM webhook_endpoints WHERE id = sqlc.arg(id);

-- name: WebhookEndpointOrg :one
-- Any organization's endpoint, through the definer; no row for none.
SELECT o.org::uuid AS org_id FROM goal_webhook_endpoint_org(sqlc.arg(id)::uuid) AS o (org) WHERE o.org IS NOT NULL;

-- name: StaleSealedWebhookSecrets :many
-- goal db rekey, as goal_owner: a batch of endpoints sealed under another key than the active one,
-- in every organization, locked.
SELECT id, org_id, coalesce(key_id, '')::text AS key_id, coalesce(nonce, ''::bytea)::bytea AS nonce,
       coalesce(ciphertext, ''::bytea)::bytea AS ciphertext
FROM webhook_endpoints
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

-- name: InsertReceivedDelivery :one
-- A delivery the receiver keeps a row of (00118): an accepted one with its id, event and body, or
-- a refused one with metadata only.
INSERT INTO webhook_deliveries (id, endpoint_id, received_at, headers, body, outcome, reason, delivery_id, event, receipt)
VALUES (sqlc.arg(id), sqlc.arg(endpoint_id), sqlc.arg(received_at), sqlc.arg(headers), sqlc.arg(body), sqlc.arg(outcome),
        sqlc.arg(reason), sqlc.narg(delivery_id), sqlc.arg(event), sqlc.arg(receipt))
RETURNING id;

-- name: InsertLedgerID :one
-- The webhook's ledger takes an accepted delivery id; no row when it already holds it.
INSERT INTO webhook_delivery_ids (webhook_id, delivery_id, received_at)
VALUES (sqlc.arg(webhook_id), sqlc.arg(delivery_id), sqlc.arg(received_at))
ON CONFLICT (org_id, webhook_id, delivery_id) DO NOTHING
RETURNING delivery_id;

-- name: CountDuplicate :one
-- The row a delivery id first came with counts one more duplicate; no row when there is none.
UPDATE webhook_deliveries SET duplicates = duplicates + 1
WHERE endpoint_id = sqlc.arg(endpoint_id) AND delivery_id = sqlc.arg(delivery_id)::text
RETURNING id;

-- name: CountUnnamedDelivery :exec
-- One more delivery no enabled trigger named, on its UTC day.
INSERT INTO webhook_delivery_counts (webhook_id, day, count)
VALUES (sqlc.arg(webhook_id), (sqlc.arg(received_at)::timestamptz AT TIME ZONE 'UTC')::date, 1)
ON CONFLICT (org_id, webhook_id, day) DO UPDATE SET count = webhook_delivery_counts.count + 1;

-- name: UnnamedDeliveryDays :many
-- A webhook's daily counts of deliveries no trigger named, from the UTC day of the instant on.
SELECT webhook_id, (day::timestamp AT TIME ZONE 'UTC')::timestamptz AS day, count FROM webhook_delivery_counts
WHERE webhook_id = sqlc.arg(webhook_id) AND day >= (sqlc.arg(since)::timestamptz AT TIME ZONE 'UTC')::date
ORDER BY day;

-- name: WebhookDeliveries :many
-- An endpoint's deliveries newest first, after the cursor (received_at, id); a zero cursor, an
-- id of all zeros at the latest instant, starts at the newest.
SELECT id, endpoint_id, received_at, headers, body, outcome, reason, item_id, delivery_id, event, receipt, duplicates FROM webhook_deliveries
WHERE endpoint_id = sqlc.arg(endpoint_id)
  AND (sqlc.narg(after_at)::timestamptz IS NULL OR (received_at, id) < (sqlc.narg(after_at)::timestamptz, sqlc.arg(after_id)::uuid))
ORDER BY received_at DESC, id DESC
LIMIT sqlc.arg(max_rows);

-- name: WebhookDelivery :one
SELECT id, endpoint_id, received_at, headers, body, outcome, reason, item_id, delivery_id, event, receipt, duplicates FROM webhook_deliveries
WHERE endpoint_id = sqlc.arg(endpoint_id) AND id = sqlc.arg(id);

-- name: WebhookDeliveryBySender :one
-- The webhook's kept delivery with the sender's id, for trying a trigger on it.
SELECT id, endpoint_id, received_at, headers, body, outcome, reason, item_id, delivery_id, event, receipt, duplicates FROM webhook_deliveries
WHERE endpoint_id = sqlc.arg(endpoint_id) AND delivery_id = sqlc.arg(delivery_id)::text;

-- name: WebhookDeliveryDays :many
-- The factory's endpoints' deliveries since the instant, per endpoint and UTC day; each endpoint's
-- rows come from webhook_deliveries_endpoint_idx (endpoint_id, received_at).
SELECT d.endpoint_id, date_trunc('day', d.received_at, 'UTC')::timestamptz AS day,
       count(*) FILTER (WHERE d.outcome <> 'refused') AS delivered,
       count(*) FILTER (WHERE d.outcome = 'refused') AS refused
FROM webhook_endpoints e
JOIN webhook_deliveries d ON d.endpoint_id = e.id AND d.received_at >= sqlc.arg(since)
WHERE CASE WHEN sqlc.narg(factory_id)::uuid IS NULL THEN e.factory_id IS NULL
           ELSE e.factory_id = sqlc.narg(factory_id)::uuid
                OR EXISTS (SELECT 1 FROM webhook_openings o WHERE o.webhook_id = e.id AND o.factory_id = sqlc.narg(factory_id)::uuid)
      END
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: PruneWebhookDeliveries :one
-- Every organization's deliveries received before the instant, through the definer.
SELECT goal_prune_webhook_deliveries(sqlc.arg(before)::timestamptz)::bigint AS deleted;

-- name: PruneWebhookLedger :one
-- Every organization's ledger ids received before the instant and counts of days before it,
-- through the definer.
SELECT goal_prune_webhook_ledger(sqlc.arg(before)::timestamptz)::bigint AS deleted;

-- name: InsertTriggerFiring :one
-- Returns nothing (no rows) when the trigger already has a firing with this key.
INSERT INTO trigger_firings (id, factory_id, trigger, source, event_key, idempotency_key, dedupe, outcome, reason, item_id, missed, at, check_state, try)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(trigger), sqlc.arg(source), sqlc.arg(event_key), sqlc.arg(idempotency_key),
        sqlc.arg(dedupe), sqlc.arg(outcome), sqlc.arg(reason), sqlc.narg(item_id), sqlc.arg(missed), sqlc.arg(at), sqlc.narg(check_state), sqlc.arg(try))
ON CONFLICT (org_id, factory_id, trigger, idempotency_key) DO NOTHING
RETURNING id;

-- name: TriggerFiringByKey :one
SELECT id, factory_id, trigger, source, event_key, idempotency_key, dedupe, outcome, reason, item_id, missed, at,
       check_state, run_id, cost_usd::float8 AS cost_usd, decisions, try, applied, checked_at FROM trigger_firings
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
SELECT f.id, f.factory_id, f.trigger, f.source, f.event_key, f.idempotency_key, f.dedupe, f.outcome, f.reason, f.item_id, f.missed, f.at,
       f.check_state, f.run_id, f.cost_usd::float8 AS cost_usd, f.decisions,
       coalesce(w.number, 0)::bigint AS item_number,
       EXISTS (SELECT 1 FROM item_events e WHERE e.item_id = f.item_id AND e.kind = 'merged') AS merged
FROM trigger_firings f
LEFT JOIN work_items w ON w.id = f.item_id
WHERE f.factory_id = sqlc.arg(factory_id) AND f.trigger = sqlc.arg(trigger)
ORDER BY f.at DESC, f.id DESC
LIMIT sqlc.arg(max_rows);

-- name: PruneTriggerFirings :execrows
-- The factory's firings from before the instant, but for those whose item is still open, which
-- only.dedupe reads (ADR 0040 decision 16).
DELETE FROM trigger_firings f
WHERE f.factory_id = sqlc.arg(factory_id) AND f.at < sqlc.arg(before)
  AND NOT EXISTS (SELECT 1 FROM work_items w WHERE w.id = f.item_id AND w.state NOT IN ('done', 'cancelled'))
  AND (f.check_state IS NULL OR f.check_state NOT IN ('queued', 'running'));
