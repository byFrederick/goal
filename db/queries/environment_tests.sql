-- Environment tests (ports.EnvironmentTests; migrations 00087 and 00129), under row-level security: every
-- statement sees the transaction's organization only.

-- name: UpsertEnvironmentTest :execrows
-- org_id comes from its default, goal_org(); a factory the organization does not have violates
-- the foreign key.
INSERT INTO environment_tests (factory_id, environment, server, at, ok, error, tools, read_only_tools)
VALUES (sqlc.arg(factory_id), sqlc.arg(environment), sqlc.arg(server), sqlc.arg(at), sqlc.arg(ok), sqlc.arg(error), sqlc.arg(tools)::text[],
  sqlc.arg(read_only_tools)::text[])
ON CONFLICT (org_id, factory_id, environment, server)
DO UPDATE SET at = EXCLUDED.at, ok = EXCLUDED.ok, error = EXCLUDED.error, tools = EXCLUDED.tools,
  read_only_tools = EXCLUDED.read_only_tools;

-- name: EnvironmentTests :many
-- The organization's latest tests, or one factory's when factory_id is set.
SELECT * FROM environment_tests
WHERE sqlc.narg(factory_id)::uuid IS NULL OR factory_id = sqlc.narg(factory_id)::uuid
ORDER BY factory_id, environment, server;
