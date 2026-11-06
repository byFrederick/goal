-- +goose Up
-- Connections go (ADR 0037 decision 18): the release that converted every factory, standing rule
-- and knowledge source to secrets ran first, so this one drops connections and connection_kinds
-- (a connection's test results are its own columns, 00051 and 00075, and go with it), the
-- connection keys of standing rules and knowledge sources, and connections from
-- goal_connection_key_ids. factory_conversions stays: Needs you still reads a refused factory's
-- problems. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- A database the conversion has not run on would lose what it still reads from connections, so
-- the migration refuses it before it drops anything: an organization with a connection and a
-- published factory that has no conversion row, an active rule keyed by a connection, or a source
-- that reads a connection without a secret (a live one, or a synced one not converted).
-- +goose StatementBegin
DO $$
DECLARE
  remedy CONSTANT text := 'run a release that converts connections (ADR 0037) first, then this one';
BEGIN
  IF EXISTS (SELECT 1 FROM factories f
             WHERE EXISTS (SELECT 1 FROM connections c WHERE c.org_id = f.org_id)
               AND EXISTS (SELECT 1 FROM definition_versions v
                           WHERE v.org_id = f.org_id AND v.factory_id = f.id AND v.state <> 'draft')
               AND NOT EXISTS (SELECT 1 FROM factory_conversions fc WHERE fc.org_id = f.org_id AND fc.factory_id = f.id)) THEN
    RAISE EXCEPTION 'migration 00133 drops connections, but a factory was never converted from them: %', remedy;
  END IF;
  IF EXISTS (SELECT 1 FROM standing_rules WHERE connection_id IS NOT NULL AND revoked_at IS NULL) THEN
    RAISE EXCEPTION 'migration 00133 drops connections, but an active standing rule is still keyed by one: %', remedy;
  END IF;
  IF EXISTS (SELECT 1 FROM knowledge_sources WHERE connection_id IS NOT NULL AND secret_id IS NULL) THEN
    RAISE EXCEPTION 'migration 00133 drops connections, but a knowledge source still reads one without a secret: %', remedy;
  END IF;
END
$$;
-- +goose StatementEnd

-- Standing rules are keyed by factory, environment and server only (00091). The conversion
-- copied every active connection-keyed rule onto that key; the revoked ones it left go, as no
-- key is left to show them by.
DELETE FROM standing_rules WHERE connection_id IS NOT NULL;
DROP INDEX standing_rules_active_key;
DROP INDEX standing_rules_org_connection_idx;
ALTER TABLE standing_rules
  DROP CONSTRAINT standing_rules_key_check,
  DROP COLUMN connection_id,
  ALTER COLUMN factory_id SET NOT NULL,
  ALTER COLUMN environment SET NOT NULL,
  ALTER COLUMN server SET NOT NULL;
-- The list and the purge by organization read this index; standing_rules_active_server_key
-- (00091) keeps one active rule per key.
CREATE INDEX standing_rules_org_idx ON standing_rules (org_id);

-- Knowledge sources read with a secret or nothing. 00095's reads check without connection_id:
-- an upload reads nothing, a repository with a secret or none (a public one), Confluence and a
-- synced source with a secret; a live source read only a connection, so none is left.
ALTER TABLE knowledge_sources DROP CONSTRAINT knowledge_sources_reads_check;
REVOKE UPDATE (connection_id) ON knowledge_sources FROM goal_app;
DROP INDEX knowledge_sources_connection_idx;
ALTER TABLE knowledge_sources DROP COLUMN connection_id;
ALTER TABLE knowledge_sources ADD CONSTRAINT knowledge_sources_reads_check CHECK (CASE kind
  WHEN 'upload' THEN secret_id IS NULL
  WHEN 'repository' THEN true
  WHEN 'confluence' THEN secret_id IS NOT NULL
  WHEN 'synced' THEN secret_id IS NOT NULL
  ELSE false END);

-- goal_connection_key_ids are the master key ids some secret (its value or its previous one) or
-- webhook endpoint is sealed under, in every organization: goald refuses to start without each of
-- them. Ids only, never a sealed value. Replacing it keeps 00051's grants.
CREATE OR REPLACE FUNCTION goal_connection_key_ids() RETURNS text[]
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(array_agg(DISTINCT k.key_id ORDER BY k.key_id), '{}')
  FROM (SELECT key_id FROM secrets
        UNION ALL
        SELECT previous_key_id FROM secrets WHERE previous_key_id IS NOT NULL
        UNION ALL
        SELECT key_id FROM webhook_endpoints WHERE key_id IS NOT NULL) k
$$;

-- The tables take their policies, grants, index and trigger with them; the functions only they
-- used go after.
DROP TABLE connections;
DROP TABLE connection_kinds;
DROP FUNCTION goal_sync_platform_kinds(jsonb);
DROP FUNCTION goal_connection_kind_of_org();
