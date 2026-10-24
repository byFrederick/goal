-- +goose Up
-- Phase 3 (3-5): the organization lifecycle (spec 3 §6.3). goald stays the only writer of
-- organizations, and every change reaches Better Auth's auth.organization in the same transaction:
-- goal_provision_org creates both rows, the rename and retire triggers follow organizations, and
-- goal_purge_org deletes Better Auth's rows with Goal's. Better Auth's own organization create,
-- update and delete endpoints are off (auth/src/auth.ts disabledPaths), so nothing writes
-- auth.organization another way. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- Goal's rules on Better Auth's rows. Better Auth enforces most of them in code; these hold under
-- concurrency and against bugs. The tables are new in 00041 and empty until sign-in exists, so
-- the constraints are added and checked at once. One role per member and invitation: Better Auth
-- would accept "admin,member", which no Goal check understands.
ALTER TABLE auth.member     ADD CONSTRAINT member_role_check       CHECK (role IN ('owner', 'admin', 'member'));
ALTER TABLE auth.invitation ADD CONSTRAINT invitation_role_check   CHECK (role IN ('owner', 'admin', 'member'));
ALTER TABLE auth.invitation ADD CONSTRAINT invitation_status_check CHECK (status IN ('pending', 'accepted', 'rejected', 'canceled'));
-- A person is a member of an organization once (Better Auth checks it before inserting, which two
-- concurrent acceptances can both pass). 00043's factory_roles references it. The generated
-- session(token) unique, session("userId"), member("userId") and invitation("organizationId")
-- indexes exist already (00041).
CREATE UNIQUE INDEX member_org_user_key  ON auth.member ("organizationId", "userId");
-- The sign-in gate and the code sender look up pending invitations by address (3-8).
CREATE INDEX invitation_email_status_idx ON auth.invitation (lower(email), status);

-- Writes one audit_log entry of p_org for the definers and triggers of the identity migrations,
-- which run as goal_owner (so no row-level security binds them to an organization): the actor
-- comes from its ref (domain.Actor.Ref) with the name ActorFromRef gives it, except that a user's
-- name is read from auth."user" at that moment. Not a definer, and nobody may call it: goal_app
-- appends its own entries under row-level security (appendAudit). 00046 replaces it to name API
-- keys.
-- +goose StatementBegin
CREATE FUNCTION goal_append_audit(p_org uuid, p_by text, p_action text, p_target_kind text, p_target_id text,
                                  p_data jsonb, p_request_id text) RETURNS void
LANGUAGE plpgsql VOLATILE SET search_path FROM CURRENT
AS $$
DECLARE
  v_kind text := split_part(p_by, ':', 1);
  v_id   text := substr(p_by, length(split_part(p_by, ':', 1)) + 2);
  v_name text;
BEGIN
  IF v_id IS NULL OR v_id = '' THEN
    RAISE EXCEPTION 'actor ref % is not <kind>:<id>', p_by USING ERRCODE = 'check_violation';
  END IF;
  IF p_by = 'user:local' THEN
    v_name := 'Local user';
  ELSIF v_kind = 'user' AND v_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
    SELECT u.name INTO v_name FROM auth."user" u WHERE u.id = v_id::uuid;
  END IF;
  INSERT INTO audit_log (org_id, actor_kind, actor_id, actor_name, action, target_kind, target_id, data, request_id)
  VALUES (p_org, v_kind, v_id, coalesce(v_name, v_id), p_action, p_target_kind, p_target_id,
          coalesce(p_data, '{}'::jsonb), p_request_id);
END
$$;
-- +goose StatementEnd

-- An organization keeps at least one owner. Owner changes of one organization are serialized on
-- its auth.organization row, then counted at commit, so two owners demoting each other at once
-- cannot leave none (READ COMMITTED, Better Auth's level and goald's: each statement of the
-- function sees what committed before it got the lock). FOR NO KEY UPDATE conflicts with itself
-- but not with the key-share lock a new member's foreign key takes, so invitations being accepted
-- meanwhile do not wait. An organization that is deleted (or being purged: goal_purge_org deletes
-- the organizations row first, and auth.organization with it) needs no owner.
-- +goose StatementBegin
CREATE FUNCTION goal_keep_an_owner() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  PERFORM 1 FROM auth.organization WHERE id = OLD."organizationId" FOR NO KEY UPDATE;
  IF NOT FOUND OR NOT EXISTS (SELECT 1 FROM organizations
                              WHERE id = OLD."organizationId" AND deleted_at IS NULL) THEN
    RETURN NULL;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM auth.member
                 WHERE "organizationId" = OLD."organizationId" AND role = 'owner') THEN
    RAISE EXCEPTION 'organization % must keep an owner', OLD."organizationId"
      USING ERRCODE = 'check_violation', CONSTRAINT = 'member_keeps_an_owner';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER member_keeps_an_owner AFTER UPDATE OF role OR DELETE ON auth.member
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (OLD.role = 'owner')
  EXECUTE FUNCTION goal_keep_an_owner();

-- Creates an organization in the caller's transaction: its organizations row, Better Auth's
-- auth.organization row (same id, name and slug) and, when p_owner is set, the owner's
-- auth.member row (id p_owner_member, changedBy p_by); then org.created by p_by. Ids come from Go
-- (UUIDv7). A rule the rows break (slug taken, CHECK) fails the whole call, so both rows exist or
-- neither does. goal_app may no longer insert organizations itself (below).
-- +goose StatementBegin
CREATE FUNCTION goal_provision_org(p_id uuid, p_slug text, p_name text, p_mark text, p_owner uuid,
                                   p_owner_member uuid, p_by text, p_request_id text) RETURNS SETOF organizations
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  o organizations;
BEGIN
  IF p_owner IS NOT NULL AND p_owner_member IS NULL THEN
    RAISE EXCEPTION 'the owner of organization % needs a member id', p_id USING ERRCODE = 'check_violation';
  END IF;
  INSERT INTO organizations (id, slug, name, mark) VALUES (p_id, p_slug, p_name, p_mark) RETURNING * INTO o;
  INSERT INTO auth.organization (id, name, slug, "createdAt") VALUES (o.id, o.name, o.slug, o.created_at);
  PERFORM goal_append_audit(o.id, p_by, 'org.created', 'org', o.id::text,
                            jsonb_build_object('slug', o.slug, 'name', o.name), p_request_id);
  IF p_owner IS NOT NULL THEN
    INSERT INTO auth.member (id, "organizationId", "userId", role, "createdAt", "changedBy", "changedAt")
    VALUES (p_owner_member, o.id, p_owner, 'owner', o.created_at, p_by, clock_timestamp());
  END IF;
  RETURN NEXT o;
END
$$;
-- +goose StatementEnd

-- A renamed organization (UpdateOrg) is renamed in Better Auth in the same transaction. Every
-- organization has its auth.organization row (goal_provision_org, the backfill below), so a
-- missing one is a broken invariant, not something to skip.
-- +goose StatementBegin
CREATE FUNCTION goal_rename_org() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  UPDATE auth.organization SET name = NEW.name, slug = NEW.slug WHERE id = NEW.id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'organization % has no auth.organization row', NEW.id USING ERRCODE = 'no_data_found';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER organizations_rename AFTER UPDATE OF slug, name ON organizations
  FOR EACH ROW WHEN (OLD.slug IS DISTINCT FROM NEW.slug OR OLD.name IS DISTINCT FROM NEW.name)
  EXECUTE FUNCTION goal_rename_org();

-- A soft-deleted organization (DeleteOrg) can no longer be joined: its pending invitations are
-- canceled in the same transaction, stamped system:org-deleted (00043 audits them). 00046 also
-- revokes its API keys here. Sessions lose the organization at once because goal_principal
-- ignores deleted organizations (00045).
-- +goose StatementBegin
CREATE FUNCTION goal_retire_org() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  UPDATE auth.invitation SET status = 'canceled', "changedBy" = 'system:org-deleted', "changedAt" = clock_timestamp()
  WHERE "organizationId" = NEW.id AND status = 'pending';
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER organizations_retire AFTER UPDATE OF deleted_at ON organizations
  FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
  EXECUTE FUNCTION goal_retire_org();

-- Hands the transaction's organization (goal_org()) from the owner p_from to the admin p_to: p_to
-- becomes owner and p_from admin (changedBy p_by), then org.ownership_transferred {from, to}. The
-- organization must be live (no_data_found otherwise, so ErrNotFound); p_from must be an owner
-- and p_to an admin, or check_violation names which (goal_transfer_from_owner,
-- goal_transfer_to_admin). The organization's auth.organization row is locked first, as
-- goal_keep_an_owner locks it, and the organizations row is share-locked so a soft delete waits.
-- +goose StatementBegin
CREATE FUNCTION goal_transfer_ownership(p_from uuid, p_to uuid, p_by text, p_request_id text) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  v_org uuid := goal_org();
BEGIN
  PERFORM 1 FROM organizations WHERE id = v_org AND deleted_at IS NULL FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'organization % does not exist', v_org USING ERRCODE = 'no_data_found';
  END IF;
  PERFORM 1 FROM auth.organization WHERE id = v_org FOR NO KEY UPDATE;
  PERFORM 1 FROM auth.member WHERE "organizationId" = v_org AND "userId" = p_from AND role = 'owner' FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'user % is not an owner of organization %', p_from, v_org
      USING ERRCODE = 'check_violation', CONSTRAINT = 'goal_transfer_from_owner';
  END IF;
  UPDATE auth.member SET role = 'owner', "changedBy" = p_by, "changedAt" = clock_timestamp()
  WHERE "organizationId" = v_org AND "userId" = p_to AND role = 'admin';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'user % is not an admin of organization %', p_to, v_org
      USING ERRCODE = 'check_violation', CONSTRAINT = 'goal_transfer_to_admin';
  END IF;
  UPDATE auth.member SET role = 'admin', "changedBy" = p_by, "changedAt" = clock_timestamp()
  WHERE "organizationId" = v_org AND "userId" = p_from;
  PERFORM goal_append_audit(v_org, p_by, 'org.ownership_transferred', 'org', v_org::text,
                            jsonb_build_object('from', p_from, 'to', p_to), p_request_id);
END
$$;
-- +goose StatementEnd

-- The purge of 00037, unchanged (retention checked inside, false when not due, SKIP LOCKED, one
-- organization per transaction), then Better Auth's rows of the organization. They go after the
-- organizations row, so their triggers see no organization: goal_keep_an_owner asks for no owner
-- and 00043's audit triggers write nothing. CREATE OR REPLACE keeps goal_app's EXECUTE.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION goal_purge_org(p_org uuid, p_before timestamptz) RETURNS boolean
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
  DELETE FROM auth.invitation WHERE "organizationId" = p_org;
  DELETE FROM auth.member     WHERE "organizationId" = p_org;
  DELETE FROM auth.organization WHERE id = p_org;
  RETURN true;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION goal_append_audit(uuid, text, text, text, text, jsonb, text), goal_keep_an_owner(),
  goal_provision_org(uuid, text, text, text, uuid, uuid, text, text), goal_rename_org(), goal_retire_org(),
  goal_transfer_ownership(uuid, uuid, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_provision_org(uuid, text, text, text, uuid, uuid, text, text),
  goal_transfer_ownership(uuid, uuid, text, text) TO goal_app;
-- Organizations are created only with their auth.organization row.
REVOKE INSERT ON organizations FROM goal_app;

-- Backfill: every organization, deleted ones included (so the purge stays uniform), gets its
-- auth.organization row, default included. None has members until the bootstrap owner signs in
-- (spec 3 §14).
INSERT INTO auth.organization (id, name, slug, "createdAt")
SELECT id, name, slug, created_at FROM organizations;
