-- +goose Up
-- Review 2.2 M1, L1 and L7: the organization functions act only on what they are asked about.
-- Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- L7: no definer answers for an organization other than the transaction's. Tenancy.Orgs counts
-- each organization's factories in a transaction bound to it, under row-level security.
DROP FUNCTION goal_org_factory_count(uuid);

-- M1: the retention is checked where the rows are deleted, not only by goal_deleted_orgs. An
-- organization is purged only if it was deleted before p_before (the purge's cutoff: now minus
-- GOAL_ORG_RETENTION), so a soft delete followed by a purge in the same transaction purges
-- nothing. It returns false, having done nothing, for an organization that is not deleted, was
-- deleted on or after p_before, or is held by another purge (SKIP LOCKED: purges of one install
-- share the work instead of waiting for each other). The purge runs one organization per
-- transaction (internal/retention).
--
-- L1: published versions are deleted here and nowhere else. The factories' pointers are cleared
-- and the versions deleted by this function's own statements before the factories, so
-- definition_versions_keep_published sees goal_owner at trigger depth 1; a cascade from a
-- factory delete runs deeper.
DROP FUNCTION goal_purge_org(uuid);
-- +goose StatementBegin
CREATE FUNCTION goal_purge_org(p_org uuid, p_before timestamptz) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  PERFORM 1 FROM organizations WHERE id = p_org AND deleted_at < p_before FOR UPDATE SKIP LOCKED;
  IF NOT FOUND THEN
    RETURN false;
  END IF;
  DELETE FROM item_events WHERE org_id = p_org;
  DELETE FROM leases      WHERE org_id = p_org;
  DELETE FROM agent_runs  WHERE org_id = p_org;
  DELETE FROM andon_pulls WHERE org_id = p_org;
  DELETE FROM work_items  WHERE org_id = p_org;
  UPDATE factories SET current_version_id = NULL WHERE org_id = p_org AND current_version_id IS NOT NULL;
  DELETE FROM definition_versions WHERE org_id = p_org;
  DELETE FROM factories   WHERE org_id = p_org;
  DELETE FROM audit_log   WHERE org_id = p_org;
  DELETE FROM organizations WHERE id = p_org;
  RETURN true;
END
$$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION goal_purge_org(uuid, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_purge_org(uuid, timestamptz) TO goal_app;

-- L1: a published version is deleted only by goal_purge_org, for an organization that was
-- deleted. The trigger runs as the deleting role: only the table's owner (goal_owner, which
-- goald reaches through that definer alone) deleting the row with a statement of its own (trigger
-- depth 1) passes. goal_app's deletes, and the cascades of its factory deletes (which run as the
-- owner, one trigger level deeper), are refused, soft-deleted organization or not.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION definition_versions_keep_published() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.state = 'published'
     AND NOT (pg_trigger_depth() = 1
              AND current_user = (SELECT pg_get_userbyid(c.relowner) FROM pg_class c WHERE c.oid = TG_RELID)
              AND EXISTS (SELECT 1 FROM organizations WHERE id = OLD.org_id AND deleted_at IS NOT NULL)) THEN
    RAISE EXCEPTION 'published definition version % cannot be deleted', OLD.id USING ERRCODE = 'check_violation';
  END IF;
  RETURN OLD;
END
$$;
-- +goose StatementEnd
