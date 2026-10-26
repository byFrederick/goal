-- +goose Up
-- Deleting an archived factory for good (DefinitionStore.DeleteArchivedFactory). goal_app may not
-- delete items, runs, events or spans (events are append-only, 00004) nor published versions
-- (00037's L1), so the rows go through this definer alone, as an organization's go through
-- goal_purge_org. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- goal_delete_factory deletes a factory of the transaction's organization (goal_org()) with every
-- row that references it, in the order of goal_purge_org: events (their touch labels cascade),
-- spans, runs, andon pulls, items (their touches and needs cascade), then the versions and the
-- factory (its roles and API key scopes cascade). Only an archived factory none of whose items
-- holds a lease goes; the adapter checks both under the factory's row lock first, and this
-- refuses anything else with check_violation. A key scoped to the factory keeps its other
-- factories, or none (all_factories stays false, so it reaches no factory). The audit log stays.
-- +goose StatementBegin
CREATE FUNCTION goal_delete_factory(p_factory uuid) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  v_org uuid := goal_org();
BEGIN
  PERFORM 1 FROM factories WHERE org_id = v_org AND id = p_factory AND archived_at IS NOT NULL;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'factory % is not an archived factory of the organization', p_factory
      USING ERRCODE = 'check_violation';
  END IF;
  IF EXISTS (SELECT 1 FROM leases WHERE org_id = v_org AND factory_id = p_factory) THEN
    RAISE EXCEPTION 'an item of factory % holds a lease', p_factory USING ERRCODE = 'check_violation';
  END IF;
  DELETE FROM item_events WHERE org_id = v_org AND factory_id = p_factory;
  DELETE FROM lease_spans WHERE org_id = v_org AND factory_id = p_factory;
  DELETE FROM agent_runs  WHERE org_id = v_org AND factory_id = p_factory;
  DELETE FROM andon_pulls WHERE org_id = v_org AND factory_id = p_factory;
  DELETE FROM item_spans  WHERE org_id = v_org AND factory_id = p_factory;
  DELETE FROM work_items  WHERE org_id = v_org AND factory_id = p_factory;
  UPDATE factories SET current_version_id = NULL WHERE org_id = v_org AND id = p_factory;
  DELETE FROM definition_versions WHERE org_id = v_org AND factory_id = p_factory;
  DELETE FROM factories   WHERE org_id = v_org AND id = p_factory;
END
$$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION goal_delete_factory(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_delete_factory(uuid) TO goal_app;

-- A published version is deleted only by a definer's own statement (00037's L1): goal_purge_org's
-- for an organization that was deleted, and now goal_delete_factory's for an archived factory.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION definition_versions_keep_published() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.state = 'published'
     AND NOT (pg_trigger_depth() = 1
              AND current_user = (SELECT pg_get_userbyid(c.relowner) FROM pg_class c WHERE c.oid = TG_RELID)
              AND (EXISTS (SELECT 1 FROM organizations WHERE id = OLD.org_id AND deleted_at IS NOT NULL)
                   OR EXISTS (SELECT 1 FROM factories WHERE id = OLD.factory_id AND archived_at IS NOT NULL))) THEN
    RAISE EXCEPTION 'published definition version % cannot be deleted', OLD.id USING ERRCODE = 'check_violation';
  END IF;
  RETURN OLD;
END
$$;
-- +goose StatementEnd
