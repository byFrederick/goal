-- name: SlotCaps :one
-- The caps of the transaction's organization and of one of its factories (0: no cap), and the
-- leases each holds now. Always one row: a factory of another organization has no cap and no
-- leases, as row-level security hides it.
SELECT
  coalesce((SELECT o.max_slots FROM organizations o WHERE o.id = goal_org()), 0)::integer AS org_max,
  coalesce((SELECT f.max_slots FROM factories f WHERE f.id = sqlc.arg(factory_id)), 0)::integer AS factory_max,
  (SELECT count(*) FROM leases l WHERE l.org_id = goal_org())::bigint AS org_leases,
  (SELECT count(*) FROM leases l WHERE l.factory_id = sqlc.arg(factory_id))::bigint AS factory_leases;

-- name: LockOrgSlots :exec
-- Serializes the claims of one organization while a cap is set, so concurrent claims never
-- count the same free slot; released at commit.
SELECT pg_advisory_xact_lock(hashtextextended('goal.slots.' || goal_org()::text, 0));

-- name: FactorySlots :many
-- Every factory of the transaction's organization with its cap (NULL: none).
SELECT id, max_slots FROM factories ORDER BY id;

-- name: SetFactorySlots :exec
-- Sets the cap of a factory of the transaction's organization; NULL clears it.
UPDATE factories SET max_slots = sqlc.narg(max_slots), updated_at = now() WHERE id = sqlc.arg(id);
