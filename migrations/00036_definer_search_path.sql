-- +goose Up
-- Review 2.2 H1: the SECURITY DEFINER functions of 00005/00006 run as goal_owner, and their
-- search_path (SET search_path FROM CURRENT) did not name pg_temp. PostgreSQL then searches the
-- caller's temporary schema first, so a caller's temporary table named organizations could take
-- the real one's place: goal_purge_org wiped a live organization that way, and goal_orgs and
-- goal_org_by_ref returned forged rows. Every definer is replaced here with its body unchanged and
-- its search_path pinned to this schema and pg_temp last (pg_catalog, which the path does not
-- name, is searched first; naming it first would create the functions in it). goal db init also
-- revokes TEMPORARY and CONNECT on the database from PUBLIC (postgres.InitRoles).
--
-- The convention for every definer from here on: a migration that creates or replaces one first
-- sets this transaction's search_path the same way, then writes SET search_path FROM CURRENT (the
-- schema differs per install and per integration test). goose runs each migration in one
-- transaction, and so does `task migrate-check` (psql --single-transaction), so the setting ends
-- with the migration. TestSchema_EveryOrgTableHasRLS fails on a definer whose path does not end in
-- pg_temp.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE OR REPLACE FUNCTION goal_orgs() RETURNS SETOF organizations
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$ SELECT * FROM organizations WHERE deleted_at IS NULL ORDER BY name, id $$;

CREATE OR REPLACE FUNCTION goal_org_factory_count(p_org uuid) RETURNS bigint
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$ SELECT count(*) FROM factories WHERE org_id = p_org $$;

CREATE OR REPLACE FUNCTION goal_org_by_ref(p_ref text) RETURNS SETOF organizations
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$ SELECT * FROM organizations WHERE deleted_at IS NULL AND (slug = p_ref OR id::text = lower(p_ref)) $$;

CREATE OR REPLACE FUNCTION goal_active_orgs() RETURNS SETOF uuid
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$ SELECT o.id FROM organizations o WHERE o.deleted_at IS NULL ORDER BY o.id $$;

CREATE OR REPLACE FUNCTION goal_deleted_orgs(p_before timestamptz, p_limit integer) RETURNS SETOF uuid
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT o.id FROM organizations o
  WHERE o.deleted_at < p_before ORDER BY o.deleted_at, o.id LIMIT p_limit
  FOR UPDATE SKIP LOCKED
$$;

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
