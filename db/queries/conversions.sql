-- Factory conversions (ports.FactoryConversions; migration 00087), under row-level security:
-- every statement sees the transaction's organization only.

-- name: InsertFactoryConversion :execrows
-- org_id comes from its default, goal_org(). A factory that has its row already keeps it: no row
-- is inserted, and the caller learns it from the count.
INSERT INTO factory_conversions (factory_id, from_version, to_version, problems, names, at)
VALUES (sqlc.arg(factory_id), sqlc.arg(from_version), sqlc.narg(to_version), sqlc.arg(problems)::text[], sqlc.arg(names), sqlc.arg(at))
ON CONFLICT (org_id, factory_id) DO NOTHING;

-- name: FactoryConversions :many
SELECT * FROM factory_conversions ORDER BY factory_id;
