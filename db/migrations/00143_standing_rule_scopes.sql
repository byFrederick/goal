-- +goose Up
-- Standing rules' scopes are renamed (ADR 0031 decision 8, amended 2026-11-08): connection:read
-- becomes standing_rule:read and connection:manage becomes standing_rule:manage, because
-- connections are gone (ADR 0037 decision 16) and the scopes only cover standing rules. goald
-- grants only the scope names permissions.yaml lists, so a key or token that kept an old name
-- would quietly lose the grant. This rewrites the stored names in place, revoked rows included,
-- and leaves every other scope and row as it is.
UPDATE api_keys
SET scopes = array_replace(array_replace(scopes, 'connection:read', 'standing_rule:read'),
                           'connection:manage', 'standing_rule:manage')
WHERE scopes && ARRAY['connection:read', 'connection:manage']::text[];

UPDATE personal_tokens
SET scopes = array_replace(array_replace(scopes, 'connection:read', 'standing_rule:read'),
                           'connection:manage', 'standing_rule:manage')
WHERE scopes && ARRAY['connection:read', 'connection:manage']::text[];
