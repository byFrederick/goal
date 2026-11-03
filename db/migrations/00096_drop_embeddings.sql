-- +goose Up
-- Knowledge is files (ADR 0038 decision 16): nothing embeds a chunk any more, so the embeddings, the
-- model each source, document and chunk was embedded with and the HNSW index on them go. The
-- vector extension itself stays: the administrator owns it (goal db init), so no migration can
-- drop it. The chunks and documents stay, with keyword search, until upload sources hold
-- their files without them.
DROP INDEX knowledge_chunks_embedding_idx;
ALTER TABLE knowledge_chunks DROP COLUMN embedding, DROP COLUMN embed_model;
ALTER TABLE knowledge_documents DROP COLUMN embed_model;
ALTER TABLE knowledge_sources DROP COLUMN embed_model;

-- The source due first claims no longer weigh a model: not live, its lease ended, and due.
DROP FUNCTION goal_claim_knowledge_source(timestamptz, timestamptz, text);

-- +goose StatementBegin
CREATE FUNCTION goal_claim_knowledge_source(p_now timestamptz, p_until timestamptz)
RETURNS TABLE (org_id uuid, id uuid, lease_token uuid)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  UPDATE knowledge_sources s SET lease_until = p_until, status = 'syncing', lease_token = gen_random_uuid()
  WHERE s.id = (
    SELECT k.id FROM knowledge_sources k
    WHERE k.kind <> 'live' AND (k.lease_until IS NULL OR k.lease_until <= p_now)
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
