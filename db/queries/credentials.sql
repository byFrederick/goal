-- Credentials (00065), now the secrets of 00085: postgres.Credentials reads and writes them with
-- secrets.sql's queries. These are the queries only credentials have, over the connections that
-- refer to a credential as {{name}} (00070's secret_refs), under row-level security.

-- name: CredentialUsers :many
-- The connections that refer to credential name as {{name}}, by name.
SELECT c.name FROM connections c
WHERE EXISTS (SELECT 1 FROM jsonb_each_text(c.secret_refs) AS r (field, credential) WHERE r.credential = sqlc.arg(name)::text)
ORDER BY c.name;

-- name: CredentialUserCounts :many
-- How many connections refer to each of names, for an import's dry run; a name no connection
-- refers to has no row.
SELECT r.credential::text AS name, count(DISTINCT c.id)::integer AS connections
FROM connections c CROSS JOIN LATERAL jsonb_each_text(c.secret_refs) AS r (field, credential)
WHERE r.credential = ANY (sqlc.arg(names)::text[])
GROUP BY r.credential;
