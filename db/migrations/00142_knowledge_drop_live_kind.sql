-- +goose Up
-- Live sources are deleted (ADR 0038 decision 16), and 00135's reads_check already refuses every
-- live row, so the kind CHECK stops listing 'live' and nothing filters for it any more: the live
-- check (00100) goes, the next-sync index (00055) covers the sources that are due at some time,
-- and the claim (00098) no longer skips a kind that cannot exist.
ALTER TABLE knowledge_sources DROP CONSTRAINT knowledge_sources_kind_check;
ALTER TABLE knowledge_sources ADD CONSTRAINT knowledge_sources_kind_check
  CHECK (kind IN ('upload', 'repository', 'confluence', 'synced'));

ALTER TABLE knowledge_sources DROP CONSTRAINT knowledge_sources_live_check;

-- An on-demand source between syncs has a NULL next_sync_at (00100) and is never claimed.
DROP INDEX knowledge_sources_next_sync_idx;
CREATE INDEX knowledge_sources_next_sync_idx ON knowledge_sources (next_sync_at, id) WHERE next_sync_at IS NOT NULL;

-- The function pins its search_path to this schema, pg_temp last (00055).
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- The source due first: its lease ended, and due, in an organization not deleted.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION goal_claim_knowledge_source(p_now timestamptz, p_until timestamptz)
RETURNS TABLE (org_id uuid, id uuid, lease_token uuid)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  UPDATE knowledge_sources s SET lease_until = p_until, status = 'syncing', lease_token = gen_random_uuid()
  WHERE s.id = (
    SELECT k.id FROM knowledge_sources k
    WHERE (k.lease_until IS NULL OR k.lease_until <= p_now)
      AND k.next_sync_at <= p_now
      AND EXISTS (SELECT 1 FROM organizations o WHERE o.id = k.org_id AND o.deleted_at IS NULL)
    ORDER BY k.next_sync_at, k.id
    LIMIT 1
    FOR UPDATE OF k SKIP LOCKED)
  RETURNING s.org_id, s.id, s.lease_token
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION goal_claim_knowledge_source(timestamptz, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_claim_knowledge_source(timestamptz, timestamptz) TO goal_app;
