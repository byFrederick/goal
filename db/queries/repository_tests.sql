-- Repository tests (ports.RepositoryTests; migration 00131), under row-level security: every
-- statement sees the transaction's organization only.

-- name: UpsertRepositoryTest :execrows
-- org_id comes from its default, goal_org(); a factory the organization does not have violates
-- the foreign key. ci_ok and ci_reason are NULL for a test without a CI check (migration 00140).
INSERT INTO repository_tests (factory_id, url, access, secret, at, tested, ok, summary, warning, read_ok, read_reason, push_ok, push_reason,
  readable, pushable, truncated, ci_ok, ci_reason)
VALUES (sqlc.arg(factory_id), sqlc.arg(url), sqlc.arg(access), sqlc.arg(secret), sqlc.arg(at), sqlc.arg(tested), sqlc.arg(ok), sqlc.arg(summary),
  sqlc.arg(warning), sqlc.arg(read_ok), sqlc.arg(read_reason), sqlc.arg(push_ok), sqlc.arg(push_reason), sqlc.arg(readable), sqlc.arg(pushable),
  sqlc.arg(truncated), sqlc.narg(ci_ok), sqlc.narg(ci_reason))
ON CONFLICT (org_id, factory_id, url)
DO UPDATE SET access = EXCLUDED.access, secret = EXCLUDED.secret, at = EXCLUDED.at, tested = EXCLUDED.tested, ok = EXCLUDED.ok,
  summary = EXCLUDED.summary, warning = EXCLUDED.warning, read_ok = EXCLUDED.read_ok, read_reason = EXCLUDED.read_reason,
  push_ok = EXCLUDED.push_ok, push_reason = EXCLUDED.push_reason, readable = EXCLUDED.readable, pushable = EXCLUDED.pushable,
  truncated = EXCLUDED.truncated, ci_ok = EXCLUDED.ci_ok, ci_reason = EXCLUDED.ci_reason;

-- name: RepositoryTests :many
-- One factory's latest tests, by url in byte order.
SELECT * FROM repository_tests WHERE factory_id = sqlc.arg(factory_id) ORDER BY url COLLATE "C";

-- name: RepositoryTestsBySecret :many
-- The organization's latest tests whose token came from secret, by factory and url.
SELECT * FROM repository_tests WHERE secret = sqlc.arg(secret) ORDER BY factory_id, url COLLATE "C";
