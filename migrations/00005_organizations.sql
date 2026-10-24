-- +goose Up
-- Phase 2.2 A2: organizations and the audit log. Runs as goal_owner; goald connects as goal_app,
-- which row-level security binds to the organization of each transaction.

-- The organization of the current transaction: goald runs set_config('goal.org_id', $1, true) in
-- every transaction that touches org-owned rows. Unset (or reset to '' after an earlier
-- transaction) the cast fails, so a forgotten org is an error, never an empty or foreign answer.
-- Policies call it as (SELECT goal_org()) so it is evaluated once per statement, not per row.
CREATE FUNCTION goal_org() RETURNS uuid LANGUAGE sql STABLE PARALLEL SAFE
AS $$ SELECT current_setting('goal.org_id')::uuid $$;

CREATE TABLE organizations (
  id         uuid PRIMARY KEY,
  slug       text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$'),
  name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  mark       text NOT NULL CHECK (length(mark) BETWEEN 1 AND 2),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz                         -- soft delete; purged after the retention window
);
-- Unique across live and deleted orgs until purge; reserved words are refused in Go (domain.CheckSlug).
CREATE UNIQUE INDEX organizations_slug_key ON organizations (slug);

-- Append-only: goal_app may insert and read, never update or delete.
-- actor_id is a text id (domain.Actor.ID: "local", an agent name, a job), not a users FK; phase 4
-- adds users and keeps these rows readable.
CREATE TABLE audit_log (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  at          timestamptz NOT NULL DEFAULT now(),
  actor_kind  text NOT NULL CHECK (actor_kind IN ('user', 'agent', 'system')),
  actor_id    text NOT NULL,
  actor_name  text NOT NULL,
  action      text NOT NULL CHECK (action ~ '^[a-z_]+\.[a-z_]+$'),
  target_kind text NOT NULL,
  target_id   text NOT NULL,
  factory_id  uuid,                              -- no FK: the log outlives factories
  data        jsonb NOT NULL DEFAULT '{}',
  request_id  text
);
CREATE INDEX audit_log_org_idx    ON audit_log (org_id, id DESC);
CREATE INDEX audit_log_target_idx ON audit_log (org_id, target_kind, target_id, id DESC);

ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON organizations
  USING (id = (SELECT goal_org())) WITH CHECK (id = (SELECT goal_org()));
ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON audit_log
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, UPDATE ON organizations TO goal_app;      -- purge is goal_purge_org()
GRANT SELECT, INSERT ON audit_log TO goal_app;

-- Backfill: every existing row belongs to one default organization. The id is fixed so Go
-- (postgres.DefaultOrgID), 00006's backfill and the bucket migration agree.
INSERT INTO organizations (id, slug, name, mark)
VALUES ('019a1c00-0000-7000-8000-000000000001', 'default', 'Default', 'De');

-- Cross-organization reads and the purge (A §6.4): SECURITY DEFINER, so they run as goal_owner,
-- which row-level security does not bind. Each is pinned to this schema (SET search_path FROM
-- CURRENT), has no dynamic SQL and returns organization rows and ids only, never work rows.
-- goal_app may execute them; nobody else.

-- Every live organization, by name.
CREATE FUNCTION goal_orgs() RETURNS SETOF organizations
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$ SELECT * FROM organizations WHERE deleted_at IS NULL ORDER BY name, id $$;

-- How many factories an organization has. Factories carry no org_id before 00006, which
-- replaces this body to count them.
CREATE FUNCTION goal_org_factory_count(p_org uuid) RETURNS bigint
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$ SELECT 0::bigint $$;

-- The live organization whose slug or id is p_ref, or no row (unknown and deleted alike).
CREATE FUNCTION goal_org_by_ref(p_ref text) RETURNS SETOF organizations
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$ SELECT * FROM organizations WHERE deleted_at IS NULL AND (slug = p_ref OR id::text = lower(p_ref)) $$;

-- The ids of every live organization, for jobs that serve them all.
CREATE FUNCTION goal_active_orgs() RETURNS SETOF uuid
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$ SELECT o.id FROM organizations o WHERE o.deleted_at IS NULL ORDER BY o.id $$;

-- Up to p_limit organizations deleted before p_before, oldest deletion first, locked for the
-- caller's transaction; those another purge holds are skipped.
CREATE FUNCTION goal_deleted_orgs(p_before timestamptz, p_limit integer) RETURNS SETOF uuid
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT o.id FROM organizations o
  WHERE o.deleted_at < p_before ORDER BY o.deleted_at, o.id LIMIT p_limit
  FOR UPDATE SKIP LOCKED
$$;

-- Hard-deletes a deleted organization and every row it owns, children first. Later migrations
-- CREATE OR REPLACE it to delete their tables' rows first, or rely on ON DELETE CASCADE.
-- +goose StatementBegin
CREATE FUNCTION goal_purge_org(p_org uuid) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  PERFORM 1 FROM organizations WHERE id = p_org AND deleted_at IS NOT NULL FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'organization % is not deleted', p_org USING ERRCODE = 'no_data_found';
  END IF;
  DELETE FROM audit_log WHERE org_id = p_org;
  DELETE FROM organizations WHERE id = p_org;
END
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION goal_orgs(), goal_org_factory_count(uuid), goal_org_by_ref(text), goal_active_orgs(),
  goal_deleted_orgs(timestamptz, integer), goal_purge_org(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_orgs(), goal_org_factory_count(uuid), goal_org_by_ref(text), goal_active_orgs(),
  goal_deleted_orgs(timestamptz, integer), goal_purge_org(uuid) TO goal_app;
