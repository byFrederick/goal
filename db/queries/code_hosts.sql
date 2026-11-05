-- The organization's added code hosts (00127), under row-level security: every statement sees
-- the transaction's organization only.

-- name: CodeHosts :many
-- In byte order, as Go sorts them, whatever the database's collation.
SELECT host, kind FROM code_hosts ORDER BY host COLLATE "C";

-- name: LockCodeHosts :exec
-- Serializes changes to the organization's hosts until the commit, so two saves never mix.
SELECT pg_advisory_xact_lock(hashtextextended('goal.code-hosts:' || goal_org()::text, 0));

-- name: DeleteCodeHostsNotIn :exec
DELETE FROM code_hosts WHERE NOT (host = ANY (sqlc.arg(hosts)::text[]));

-- name: PutCodeHost :exec
-- org_id comes from its default, goal_org(); an existing host keeps who added it and when.
INSERT INTO code_hosts (host, kind, created_by)
VALUES (sqlc.arg(host), sqlc.arg(kind), sqlc.arg(created_by))
ON CONFLICT (org_id, host) DO UPDATE SET kind = excluded.kind WHERE code_hosts.kind <> excluded.kind;

-- name: MarkCodeHostsSeeded :execrows
-- One row the first time for the organization (00128), none after: the caller seeds only on 1.
INSERT INTO code_hosts_seeded DEFAULT VALUES ON CONFLICT (org_id) DO NOTHING;

-- name: AddCodeHost :execrows
-- Adds host unless it is listed, keeping a listed host's kind and who added it.
INSERT INTO code_hosts (host, kind, created_by)
VALUES (sqlc.arg(host), sqlc.arg(kind), sqlc.arg(created_by))
ON CONFLICT (org_id, host) DO NOTHING;
