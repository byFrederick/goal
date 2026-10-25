-- +goose Up
-- Phase 4 review (review 4 SQL M1, Go M3 and M5): fenced knowledge leases, claimed across
-- organizations in one order.
--
-- A sync's lease was its lease_until alone, so a goald that stalled past it (a paused VM, a
-- renewal stuck on a dead connection) still renewed, marked seen, deleted unseen documents and
-- finished over the sync of the goald that took the source over: it could delete the documents
-- that sync had just seen, or clear its lease. Each claim now draws a lease_token; renewals, seen
-- marks, deletions and the end name it, and none applies once another claim drew a new one. The
-- end clears it.
--
-- A goald also claimed organization by organization, draining each before the next, and asked
-- every organization on every tick. goal_claim_knowledge_source claims the source due first of
-- every live organization's in one statement (oldest due first, so none waits for another's to
-- drain; nothing due is one statement), as goal_owner since it reads across organizations, and
-- returns the organization the sync then runs under. It writes only the lease of the source it
-- claims. Its order has an index of its own; the per-organization one served only the old claim.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

ALTER TABLE knowledge_sources ADD COLUMN lease_token uuid;
GRANT UPDATE (lease_token) ON knowledge_sources TO goal_app;

DROP INDEX knowledge_sources_due_idx;
CREATE INDEX knowledge_sources_next_sync_idx ON knowledge_sources (next_sync_at, id) WHERE kind <> 'live';

-- The source due first: not live, its lease ended, and due or, with a model, of another model's
-- chunks unless its last sync failed. Leased until p_until, as syncing, under a new token.
CREATE FUNCTION goal_claim_knowledge_source(p_now timestamptz, p_until timestamptz, p_model text)
RETURNS TABLE (org_id uuid, id uuid, lease_token uuid)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  UPDATE knowledge_sources s SET lease_until = p_until, status = 'syncing', lease_token = gen_random_uuid()
  WHERE s.id = (
    SELECT k.id FROM knowledge_sources k
    WHERE k.kind <> 'live' AND (k.lease_until IS NULL OR k.lease_until <= p_now)
      AND (k.next_sync_at <= p_now
           OR (p_model <> '' AND k.embed_model IS DISTINCT FROM p_model AND k.status <> 'failed'))
      AND EXISTS (SELECT 1 FROM organizations o WHERE o.id = k.org_id AND o.deleted_at IS NULL)
    ORDER BY k.next_sync_at, k.id
    LIMIT 1
    FOR UPDATE OF k SKIP LOCKED)
  RETURNING s.org_id, s.id, s.lease_token
$$;

REVOKE ALL ON FUNCTION goal_claim_knowledge_source(timestamptz, timestamptz, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_claim_knowledge_source(timestamptz, timestamptz, text) TO goal_app;
