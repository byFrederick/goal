-- Factory conversions (ports.FactoryConversions; migration 00088), under row-level security:
-- every statement sees the transaction's organization only.

-- name: InsertFactoryConversion :execrows
-- org_id comes from its default, goal_org(). A refused factory's row is replaced by the new
-- try (migration 00090 grants the UPDATE); a converted one keeps its row: nothing is written, and
-- the caller learns it from the count.
INSERT INTO factory_conversions (factory_id, from_version, to_version, problems, names, at)
VALUES (sqlc.arg(factory_id), sqlc.arg(from_version), sqlc.narg(to_version), sqlc.arg(problems)::text[], sqlc.arg(names), sqlc.arg(at))
ON CONFLICT (org_id, factory_id) DO UPDATE
SET from_version = excluded.from_version, to_version = excluded.to_version, problems = excluded.problems,
    names = excluded.names, at = excluded.at
WHERE factory_conversions.to_version IS NULL;

-- name: FactoryConversions :many
SELECT * FROM factory_conversions ORDER BY factory_id;
