-- +goose Up
-- The old intake forms go (ADR 0040 decision 24, the contract step): the release that moved every
-- factory webhook endpoint to the organization (00116 to 00118, and the conversion in goald) and
-- stopped writing work_items.source ran first, so this one drops work_items.source and the
-- factory endpoints' columns (their sealed key_id, nonce and ciphertext, their kind and their
-- factory_id) with the checks 00116 added for them, and webhook endpoints from
-- goal_connection_key_ids. Every webhook then has a sender and a secret, and its name is unique in
-- its organization. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- A database that release has not run on would lose the webhooks it still reads, so the migration
-- refuses it before it changes anything: an endpoint without a sender or a secret (a factory
-- endpoint never converted), or one that still belongs to a factory.
-- +goose StatementBegin
DO $$
DECLARE
  remedy CONSTANT text := 'run ADR 0040''s release first, then this one';
BEGIN
  IF EXISTS (SELECT 1 FROM webhook_endpoints WHERE sender IS NULL OR secret_id IS NULL) THEN
    RAISE EXCEPTION 'migration 00136 drops the factory webhook endpoints'' sealed secrets, but an endpoint has no sender or secret: %', remedy;
  END IF;
  IF EXISTS (SELECT 1 FROM webhook_endpoints WHERE factory_id IS NOT NULL) THEN
    RAISE EXCEPTION 'migration 00136 drops webhook_endpoints.factory_id, but an endpoint still belongs to a factory: %', remedy;
  END IF;
END
$$;
-- +goose StatementEnd

-- Where an item came from is its origin and via (00113); nothing reads or writes source.
ALTER TABLE work_items
  DROP CONSTRAINT work_items_source_check,
  DROP COLUMN source;

-- The checks that let a row be a factory endpoint go; the reserved names now hold for every
-- webhook, as each belongs to the organization.
ALTER TABLE webhook_endpoints
  DROP CONSTRAINT webhook_endpoints_sealed_check,
  DROP CONSTRAINT webhook_endpoints_signed_check,
  DROP CONSTRAINT webhook_endpoints_kind_or_sender_check,
  DROP CONSTRAINT webhook_endpoints_org_check,
  DROP CONSTRAINT webhook_endpoints_reserved_name_check,
  ADD CONSTRAINT webhook_endpoints_reserved_name_check CHECK (
    name NOT IN ('github', 'gitlab', 'standard', 'pagerduty', 'sentry', 'token', 'schedule', 'webhook'));

-- A webhook's name is unique in its organization; the index starts with org_id, so it is also the
-- index on org_id that 00071's (org_id, factory_id, name) unique was.
DROP INDEX webhook_endpoints_org_name_key;
CREATE UNIQUE INDEX webhook_endpoints_org_name_key ON webhook_endpoints (org_id, name);

-- The factory endpoints' columns go with their foreign key and unique; dropping a column drops
-- goal_app's grants on it (00071's rotate columns and 00118's factory_id). Every webhook has a
-- sender and a secret.
ALTER TABLE webhook_endpoints
  DROP CONSTRAINT webhook_endpoints_org_id_factory_id_fkey,
  DROP CONSTRAINT webhook_endpoints_org_id_factory_id_name_key,
  DROP COLUMN key_id,
  DROP COLUMN nonce,
  DROP COLUMN ciphertext,
  DROP COLUMN kind,
  DROP COLUMN factory_id,
  ALTER COLUMN sender SET NOT NULL,
  ALTER COLUMN secret_id SET NOT NULL;

-- goal_connection_key_ids are the master key ids some secret (its value or its previous one) is
-- sealed under, in every organization: goald refuses to start without each of them. Ids only,
-- never a sealed value. Replacing it keeps 00051's grants.
CREATE OR REPLACE FUNCTION goal_connection_key_ids() RETURNS text[]
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(array_agg(DISTINCT k.key_id ORDER BY k.key_id), '{}')
  FROM (SELECT key_id FROM secrets
        UNION ALL
        SELECT previous_key_id FROM secrets WHERE previous_key_id IS NOT NULL) k
$$;
