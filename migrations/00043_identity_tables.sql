-- +goose Up
-- Phase 3 (3-6): Goal's identity tables and the triggers on Better Auth's rows (spec 3 §6.4).
-- platform_admins and auth_events are install-level (no org, no row-level security) and reached
-- only through definers (00044, 00045); factory_roles is an org table under the tenancy kit. Every
-- membership and invitation change Better Auth makes is audited in its organization's audit_log
-- by a trigger, atomic with the change, and access changes are announced on goal_access. Definers
-- pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- Who may create organizations and manage platform admins. A user stays while an admin
-- (ON DELETE RESTRICT).
CREATE TABLE platform_admins (
  user_id  uuid PRIMARY KEY REFERENCES auth."user" (id) ON DELETE RESTRICT,
  added_by text NOT NULL CHECK (added_by ~ '^(user|service|agent|system):.+$'),   -- Actor.Ref()
  added_at timestamptz NOT NULL DEFAULT now()
);

-- The install keeps a platform admin once anyone has signed in. Removals are serialized at
-- commit by a transaction-level advisory lock, then counted: two admins removing each other at
-- once cannot leave none. The lock, not LOCK TABLE: a table lock that conflicts with itself also
-- conflicts with the row-exclusive lock each DELETE holds, so two removals would deadlock instead
-- of one being refused. goal_claim_bootstrap (00044) takes the same lock.
-- +goose StatementBegin
CREATE FUNCTION goal_keep_a_platform_admin() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('goal.platform_admins'));
  IF NOT EXISTS (SELECT 1 FROM platform_admins) AND EXISTS (SELECT 1 FROM auth."user") THEN
    RAISE EXCEPTION 'the install must keep a platform admin'
      USING ERRCODE = 'check_violation', CONSTRAINT = 'platform_admins_keep_one';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER platform_admins_keep_one AFTER DELETE ON platform_admins
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION goal_keep_a_platform_admin();

-- Factory roles of members. A role exists only for a member of the same organization and a
-- factory of it (composite keys); it goes with the membership or the factory.
CREATE TABLE factory_roles (
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id uuid NOT NULL,
  user_id    uuid NOT NULL,
  role       text NOT NULL CHECK (role IN ('viewer', 'approver', 'editor')),
  granted_by text NOT NULL CHECK (granted_by ~ '^(user|service|agent|system):.+$'),
  granted_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, factory_id, user_id),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, user_id) REFERENCES auth.member ("organizationId", "userId") ON DELETE CASCADE
);
CREATE INDEX factory_roles_user_idx ON factory_roles (org_id, user_id);
ALTER TABLE factory_roles ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON factory_roles
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
GRANT SELECT, INSERT, UPDATE, DELETE ON factory_roles TO goal_app;

-- The install's security log: sign-ins, codes, lockouts, device approvals, platform admins. No
-- org, no row-level security; written by the auth service and goald through definers only. Never
-- holds a raw email, code, token or key: subject is base64url(SHA-256(lower(email) or user code)).
CREATE TABLE auth_events (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  at         timestamptz NOT NULL DEFAULT now(),
  kind       text NOT NULL CHECK (kind ~ '^[a-z_]+\.[a-z_]+$'),
  user_id    uuid,                                   -- no FK: the log outlives users
  subject    text CHECK (subject ~ '^[A-Za-z0-9_-]{43}$'),
  ip         inet,
  user_agent text CHECK (length(user_agent) <= 512),
  data       jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX auth_events_subject_idx ON auth_events (kind, subject, at DESC);
CREATE INDEX auth_events_user_idx    ON auth_events (user_id, at DESC);
CREATE INDEX auth_events_at_idx      ON auth_events (at);

-- API keys act as actor kind service. The new check is added NOT VALID and then validated (the
-- 2.2 database review's pattern for a large table); every existing row already passes.
ALTER TABLE audit_log DROP CONSTRAINT audit_log_actor_kind_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_actor_kind_check
  CHECK (actor_kind IN ('user', 'agent', 'system', 'service')) NOT VALID;
ALTER TABLE audit_log VALIDATE CONSTRAINT audit_log_actor_kind_check;

-- Who made a change to a member or invitation row: changedBy when changedAt is within a minute of
-- now (the auth service's stamp hook sets both just before Better Auth writes the row, or a
-- definer does), else p_fallback. A stale or malformed stamp never names anyone.
-- +goose StatementBegin
CREATE FUNCTION goal_auth_actor(p_changed_by text, p_changed_at timestamptz, p_fallback text) RETURNS text
LANGUAGE sql VOLATILE SET search_path FROM CURRENT
AS $$
  SELECT CASE WHEN p_changed_by ~ '^(user|service|agent|system):.+$'
                   AND p_changed_at BETWEEN clock_timestamp() - interval '1 minute'
                                        AND clock_timestamp() + interval '1 minute'
              THEN p_changed_by ELSE p_fallback END
$$;
-- +goose StatementEnd

-- Memberships in the audit log of their organization (target member, the user's id):
-- member.added {user_id, role, via}, member.role_changed {user_id, from, to}, member.removed
-- {user_id, role, left}. A new member's actor without a stamp is the member (they accepted an
-- invitation); otherwise system:auth. via: bootstrap and recovery by their system stamp,
-- invitation when the user's accepted invitation of the organization exists (Better Auth marks it
-- accepted before it inserts the member), else org_created (goal_provision_org). Nothing is
-- written for an organization that no longer exists (the purge deletes organizations first).
-- +goose StatementBegin
CREATE FUNCTION goal_audit_member() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  v_by text;
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NOT EXISTS (SELECT 1 FROM organizations WHERE id = NEW."organizationId") THEN
      RETURN NULL;
    END IF;
    v_by := goal_auth_actor(NEW."changedBy", NEW."changedAt", 'user:' || NEW."userId");
    PERFORM goal_append_audit(NEW."organizationId", v_by, 'member.added', 'member', NEW."userId"::text,
      jsonb_build_object('user_id', NEW."userId", 'role', NEW.role, 'via', CASE
        WHEN v_by = 'system:bootstrap' THEN 'bootstrap'
        WHEN v_by = 'system:recovery' THEN 'recovery'
        WHEN EXISTS (SELECT 1 FROM auth.invitation i JOIN auth."user" u ON lower(u.email) = lower(i.email)
                     WHERE i."organizationId" = NEW."organizationId" AND u.id = NEW."userId" AND i.status = 'accepted')
          THEN 'invitation'
        ELSE 'org_created' END), NULL);
  ELSIF TG_OP = 'UPDATE' THEN
    IF NOT EXISTS (SELECT 1 FROM organizations WHERE id = NEW."organizationId") THEN
      RETURN NULL;
    END IF;
    v_by := goal_auth_actor(NEW."changedBy", NEW."changedAt", 'system:auth');
    PERFORM goal_append_audit(NEW."organizationId", v_by, 'member.role_changed', 'member', NEW."userId"::text,
      jsonb_build_object('user_id', NEW."userId", 'from', OLD.role, 'to', NEW.role), NULL);
  ELSE
    IF NOT EXISTS (SELECT 1 FROM organizations WHERE id = OLD."organizationId") THEN
      RETURN NULL;
    END IF;
    v_by := goal_auth_actor(OLD."changedBy", OLD."changedAt", 'system:auth');
    PERFORM goal_append_audit(OLD."organizationId", v_by, 'member.removed', 'member', OLD."userId"::text,
      jsonb_build_object('user_id', OLD."userId", 'role', OLD.role, 'left', v_by = 'user:' || OLD."userId"), NULL);
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER member_audit_added AFTER INSERT ON auth.member
  FOR EACH ROW EXECUTE FUNCTION goal_audit_member();
CREATE TRIGGER member_audit_role_changed AFTER UPDATE OF role ON auth.member
  FOR EACH ROW WHEN (OLD.role IS DISTINCT FROM NEW.role) EXECUTE FUNCTION goal_audit_member();
CREATE TRIGGER member_audit_removed AFTER DELETE ON auth.member
  FOR EACH ROW EXECUTE FUNCTION goal_audit_member();

-- Invitations in the audit log of their organization (target invitation, its id):
-- invitation.created {invitation_id, email, role, factory_roles} by the stamp or the inviter, and
-- invitation.accepted / .rejected / .canceled {invitation_id, email} by the stamp or system:auth.
-- Better Auth's return of a failed acceptance to pending is not an answer and is not written.
-- factoryRoles is Better Auth's text column; JSON that does not parse is kept as its text.
-- +goose StatementBegin
CREATE FUNCTION goal_audit_invitation() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  v_roles jsonb;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM organizations WHERE id = NEW."organizationId") THEN
    RETURN NULL;
  END IF;
  IF TG_OP = 'INSERT' THEN
    BEGIN
      v_roles := NEW."factoryRoles"::jsonb;
    EXCEPTION WHEN invalid_text_representation THEN
      v_roles := to_jsonb(NEW."factoryRoles");
    END;
    PERFORM goal_append_audit(NEW."organizationId",
      goal_auth_actor(NEW."changedBy", NEW."changedAt", 'user:' || NEW."inviterId"),
      'invitation.created', 'invitation', NEW.id::text,
      jsonb_build_object('invitation_id', NEW.id, 'email', NEW.email, 'role', NEW.role,
                         'factory_roles', coalesce(v_roles, '[]'::jsonb)), NULL);
  ELSE
    PERFORM goal_append_audit(NEW."organizationId",
      goal_auth_actor(NEW."changedBy", NEW."changedAt", 'system:auth'),
      'invitation.' || NEW.status, 'invitation', NEW.id::text,
      jsonb_build_object('invitation_id', NEW.id, 'email', NEW.email), NULL);
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER invitation_audit_created AFTER INSERT ON auth.invitation
  FOR EACH ROW EXECUTE FUNCTION goal_audit_invitation();
CREATE TRIGGER invitation_audit_answered AFTER UPDATE OF status ON auth.invitation
  FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status AND NEW.status IN ('accepted', 'rejected', 'canceled'))
  EXECUTE FUNCTION goal_audit_invitation();

-- Access changes on goal_access, delivered at commit: {kind, org_id?, user_id?, session_id?,
-- factory_id?} with kind member (a role changed or a membership ended), session (signed out or
-- cleaned up), factory_role (granted, changed or removed) or org (soft-deleted); 00046 adds
-- api_key. Any role may NOTIFY, so a notice only makes listeners re-read and end streams (spec 3
-- §8.8); it never grants anything. Not a definer: it reads only the row being changed.
-- +goose StatementBegin
CREATE FUNCTION goal_notify_access() RETURNS trigger
LANGUAGE plpgsql SET search_path FROM CURRENT
AS $$
DECLARE
  v_notice json;
BEGIN
  CASE TG_TABLE_NAME
  WHEN 'member' THEN
    v_notice := json_build_object('kind', 'member', 'org_id', OLD."organizationId", 'user_id', OLD."userId");
  WHEN 'session' THEN
    v_notice := json_build_object('kind', 'session', 'user_id', OLD."userId", 'session_id', OLD.id);
  WHEN 'factory_roles' THEN
    IF TG_OP = 'DELETE' THEN
      v_notice := json_build_object('kind', 'factory_role', 'org_id', OLD.org_id, 'user_id', OLD.user_id,
                                    'factory_id', OLD.factory_id);
    ELSE
      v_notice := json_build_object('kind', 'factory_role', 'org_id', NEW.org_id, 'user_id', NEW.user_id,
                                    'factory_id', NEW.factory_id);
    END IF;
  WHEN 'organizations' THEN
    v_notice := json_build_object('kind', 'org', 'org_id', NEW.id);
  END CASE;
  PERFORM pg_notify('goal_access', v_notice::text);
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER member_access_role_changed AFTER UPDATE OF role ON auth.member
  FOR EACH ROW WHEN (OLD.role IS DISTINCT FROM NEW.role) EXECUTE FUNCTION goal_notify_access();
CREATE TRIGGER member_access_removed AFTER DELETE ON auth.member
  FOR EACH ROW EXECUTE FUNCTION goal_notify_access();
CREATE TRIGGER session_access_ended AFTER DELETE ON auth.session
  FOR EACH ROW EXECUTE FUNCTION goal_notify_access();
CREATE TRIGGER factory_roles_access AFTER INSERT OR UPDATE OR DELETE ON factory_roles
  FOR EACH ROW EXECUTE FUNCTION goal_notify_access();
CREATE TRIGGER organizations_access_deleted AFTER UPDATE OF deleted_at ON organizations
  FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
  EXECUTE FUNCTION goal_notify_access();

REVOKE ALL ON FUNCTION goal_keep_a_platform_admin(), goal_auth_actor(text, timestamptz, text), goal_audit_member(),
  goal_audit_invitation(), goal_notify_access() FROM PUBLIC;
