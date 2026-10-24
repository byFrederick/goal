-- +goose Up
-- Phase 2.2 A3: every org-owned table gets org_id (existing rows: the default organization, 00005)
-- and row-level security. goal_app's grants on these tables came with 00004.

-- 1. org_id, backfilled without rewriting tables: ADD COLUMN ... NOT NULL DEFAULT <constant> is
-- a catalog-only change. New rows then take the transaction's organization.
ALTER TABLE factories   ADD COLUMN org_id uuid NOT NULL DEFAULT '019a1c00-0000-7000-8000-000000000001';
ALTER TABLE work_items  ADD COLUMN org_id uuid NOT NULL DEFAULT '019a1c00-0000-7000-8000-000000000001';
ALTER TABLE leases      ADD COLUMN org_id uuid NOT NULL DEFAULT '019a1c00-0000-7000-8000-000000000001';
ALTER TABLE agent_runs  ADD COLUMN org_id uuid NOT NULL DEFAULT '019a1c00-0000-7000-8000-000000000001';
ALTER TABLE item_events ADD COLUMN org_id uuid NOT NULL DEFAULT '019a1c00-0000-7000-8000-000000000001';
ALTER TABLE andon_pulls ADD COLUMN org_id uuid NOT NULL DEFAULT '019a1c00-0000-7000-8000-000000000001';
ALTER TABLE factories   ALTER COLUMN org_id SET DEFAULT goal_org();
ALTER TABLE work_items  ALTER COLUMN org_id SET DEFAULT goal_org();
ALTER TABLE leases      ALTER COLUMN org_id SET DEFAULT goal_org();
ALTER TABLE agent_runs  ALTER COLUMN org_id SET DEFAULT goal_org();
ALTER TABLE item_events ALTER COLUMN org_id SET DEFAULT goal_org();
ALTER TABLE andon_pulls ALTER COLUMN org_id SET DEFAULT goal_org();

-- 2. Composite keys: a row can only point at a parent of its own organization, even through a bug.
ALTER TABLE factories  ADD CONSTRAINT factories_org_fkey FOREIGN KEY (org_id) REFERENCES organizations (id) ON DELETE CASCADE;
ALTER TABLE factories  ADD CONSTRAINT factories_org_id_key UNIQUE (org_id, id);
ALTER TABLE work_items ADD CONSTRAINT work_items_org_id_key UNIQUE (org_id, id);
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_org_id_key UNIQUE (org_id, id);
ALTER TABLE work_items DROP CONSTRAINT work_items_factory_id_fkey,
  ADD CONSTRAINT work_items_factory_fkey FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id);
ALTER TABLE leases DROP CONSTRAINT leases_item_id_fkey, DROP CONSTRAINT leases_factory_id_fkey,
  ADD CONSTRAINT leases_item_fkey    FOREIGN KEY (org_id, item_id)    REFERENCES work_items (org_id, id) ON DELETE CASCADE,
  ADD CONSTRAINT leases_factory_fkey FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id);
ALTER TABLE agent_runs DROP CONSTRAINT agent_runs_item_id_fkey, DROP CONSTRAINT agent_runs_factory_id_fkey,
  ADD CONSTRAINT agent_runs_item_fkey    FOREIGN KEY (org_id, item_id)    REFERENCES work_items (org_id, id),
  ADD CONSTRAINT agent_runs_factory_fkey FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id);
ALTER TABLE item_events DROP CONSTRAINT item_events_item_id_fkey, DROP CONSTRAINT item_events_factory_id_fkey,
  DROP CONSTRAINT item_events_run_id_fkey,
  ADD CONSTRAINT item_events_item_fkey    FOREIGN KEY (org_id, item_id)    REFERENCES work_items (org_id, id),
  ADD CONSTRAINT item_events_factory_fkey FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id),
  ADD CONSTRAINT item_events_run_fkey     FOREIGN KEY (org_id, run_id)     REFERENCES agent_runs (org_id, id);
ALTER TABLE andon_pulls DROP CONSTRAINT andon_pulls_factory_id_fkey,
  ADD CONSTRAINT andon_pulls_factory_fkey FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id);
-- work_items.parent_id stays a plain FK: a parent of another organization is unreachable through
-- row-level security anyway. Purge order: goal_purge_org() deletes children first, because they
-- reference factories without ON DELETE CASCADE.

-- 3. Factory names are unique per organization; leases are counted per organization.
DROP INDEX factories_name_key;
CREATE UNIQUE INDEX factories_name_key ON factories (org_id, lower(name));
CREATE INDEX leases_org_idx ON leases (org_id);

-- 4. Row-level security, the same policy everywhere (the tenancy kit; TestSchema_EveryOrgTableHasRLS
-- guards it). Tables are not FORCE ROW LEVEL SECURITY: the owner, goal_owner, runs migrations
-- and the SECURITY DEFINER functions; goal_app owns nothing.
ALTER TABLE factories ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON factories
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE work_items ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON work_items
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE leases ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON leases
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE agent_runs ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON agent_runs
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE item_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON item_events
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE andon_pulls ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON andon_pulls
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

-- 5. Views run with the caller's rights, so row-level security applies through them.
ALTER VIEW station_wip SET (security_invoker = true);

-- 6. Notifications name the organization; the stream filters on it. The trigger runs as the
-- inserting role inside its transaction, so the item lookup is itself under row-level security.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_item_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  item_number bigint;
  item_state  text;
BEGIN
  SELECT number, state INTO item_number, item_state FROM work_items WHERE id = NEW.item_id;
  PERFORM pg_notify('goal_events', json_build_object(
    'org_id', NEW.org_id, 'id', NEW.id, 'factory_id', NEW.factory_id, 'item_id', NEW.item_id,
    'number', item_number, 'kind', NEW.kind, 'station', NEW.station, 'state', item_state, 'at', NEW.at
  )::text);
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- 7. The definer functions of 00005 that factories now concern.
CREATE OR REPLACE FUNCTION goal_org_factory_count(p_org uuid) RETURNS bigint
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$ SELECT count(*) FROM factories WHERE org_id = p_org $$;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION goal_purge_org(p_org uuid) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  PERFORM 1 FROM organizations WHERE id = p_org AND deleted_at IS NOT NULL FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'organization % is not deleted', p_org USING ERRCODE = 'no_data_found';
  END IF;
  DELETE FROM item_events WHERE org_id = p_org;
  DELETE FROM leases      WHERE org_id = p_org;
  DELETE FROM agent_runs  WHERE org_id = p_org;
  DELETE FROM andon_pulls WHERE org_id = p_org;
  DELETE FROM work_items  WHERE org_id = p_org;
  DELETE FROM factories   WHERE org_id = p_org;
  DELETE FROM audit_log   WHERE org_id = p_org;
  DELETE FROM organizations WHERE id = p_org;
END
$$;
-- +goose StatementEnd
