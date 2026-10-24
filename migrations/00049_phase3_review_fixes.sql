-- +goose Up
-- Phase 3 fixes (review L1, L2, and naming people): an invitation belongs to a live organization
-- and lives no longer than its inviter's right to send it; a person without a name is named by
-- their email.
--
-- L1: an invitation belongs to a live organization. Deleting an
-- organization cancels its pending invitations (goal_retire_org), but Better Auth's rows stay
-- until the purge, so a lingering owner or admin could still invite, opening install sign-up to
-- any address and mailing from Goal's address for the whole retention window. Now an invitation
-- is refused for an organization that is not live, atomically (a trigger, with the organization
-- row share-locked so a deletion waits and then cancels it), the auth service asks whether an
-- organization is live before inviting (goal_org_live) and counts only invitations of live
-- organizations as invitees (goal_is_invitee), and the invitations such owners made before this
-- migration are canceled. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- goal_org_live is whether p_org is an organization that has not been deleted.
CREATE FUNCTION goal_org_live(p_org uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT EXISTS (SELECT 1 FROM organizations WHERE id = p_org AND deleted_at IS NULL)
$$;

-- goal_is_invitee is whether p_email (case and surrounding spaces ignored) has a pending
-- invitation, unexpired at p_now, to a live organization: the sign-up gate's invitee (spec 3 §8.7).
CREATE FUNCTION goal_is_invitee(p_email text, p_now timestamptz) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT EXISTS (
    SELECT 1 FROM auth.invitation i
    JOIN organizations o ON o.id = i."organizationId" AND o.deleted_at IS NULL
    WHERE lower(i.email) = lower(btrim(p_email)) AND i.status = 'pending' AND i."expiresAt" > p_now)
$$;

-- goal_invite_live_org refuses an invitation to an organization that is not live
-- (check_violation goal_invitation_live_org). The organization row is share-locked, so a deletion
-- running at the same time waits for the invitation and its retire trigger then cancels it.
-- +goose StatementBegin
CREATE FUNCTION goal_invite_live_org() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  PERFORM 1 FROM organizations WHERE id = NEW."organizationId" AND deleted_at IS NULL FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'invitation to an organization that is not live' USING
      ERRCODE = 'check_violation', CONSTRAINT = 'goal_invitation_live_org';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER invitation_live_org BEFORE INSERT ON auth.invitation
  FOR EACH ROW EXECUTE FUNCTION goal_invite_live_org();

-- The pending invitations of organizations deleted before now, made by a lingering member after
-- the deletion canceled the rest: canceled as goal_retire_org would have (00043 audits them).
UPDATE auth.invitation i SET status = 'canceled', "changedBy" = 'system:org-deleted', "changedAt" = clock_timestamp()
FROM organizations o
WHERE o.id = i."organizationId" AND o.deleted_at IS NOT NULL AND i.status = 'pending';

-- L2: pending invitations outlived their inviter, so an admin about to be removed could first
-- invite their own addresses as admins, valid for 72 hours after the removal. Now, in the same
-- transaction as the change, removing an inviter (or their leaving) cancels their pending
-- invitations of the organization, stamped system:inviter-removed; making them a member cancels
-- them all, and an owner made admin loses those only an owner could send (role owner), stamped
-- system:inviter-demoted. 00043's trigger audits each cancellation.
-- +goose StatementBegin
CREATE FUNCTION goal_retire_inviter() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    UPDATE auth.invitation SET status = 'canceled', "changedBy" = 'system:inviter-removed', "changedAt" = clock_timestamp()
    WHERE "organizationId" = OLD."organizationId" AND "inviterId" = OLD."userId" AND status = 'pending';
  ELSE
    UPDATE auth.invitation SET status = 'canceled', "changedBy" = 'system:inviter-demoted', "changedAt" = clock_timestamp()
    WHERE "organizationId" = NEW."organizationId" AND "inviterId" = NEW."userId" AND status = 'pending'
      AND (NEW.role NOT IN ('owner', 'admin') OR (NEW.role <> 'owner' AND role = 'owner'));
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER member_retire_inviter_removed AFTER DELETE ON auth.member
  FOR EACH ROW EXECUTE FUNCTION goal_retire_inviter();
CREATE TRIGGER member_retire_inviter_demoted AFTER UPDATE OF role ON auth.member
  FOR EACH ROW WHEN (OLD.role IS DISTINCT FROM NEW.role) EXECUTE FUNCTION goal_retire_inviter();

-- The pending invitations whose inviter is no longer a member, or no longer an owner or admin, of
-- their organization: canceled as the trigger would have.
UPDATE auth.invitation i SET status = 'canceled', "changedBy" = 'system:inviter-removed', "changedAt" = clock_timestamp()
WHERE i.status = 'pending'
  AND NOT EXISTS (SELECT 1 FROM auth.member m WHERE m."organizationId" = i."organizationId" AND m."userId" = i."inviterId");
UPDATE auth.invitation i SET status = 'canceled', "changedBy" = 'system:inviter-demoted', "changedAt" = clock_timestamp()
FROM auth.member m
WHERE i.status = 'pending' AND m."organizationId" = i."organizationId" AND m."userId" = i."inviterId"
  AND (m.role NOT IN ('owner', 'admin') OR (m.role <> 'owner' AND i.role = 'owner'));

-- A person who signs up by code has no name until they choose one (Better Auth stores ''), so the
-- audit log and the invitations list named them with nothing. 00046's audit writer and 00045's
-- goal_invitations now name such a person by their email. CREATE OR REPLACE keeps the grants.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION goal_append_audit(p_org uuid, p_by text, p_action text, p_target_kind text, p_target_id text,
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
  ELSIF v_kind IN ('user', 'service') AND v_id !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
    v_name := NULL;
  ELSIF v_kind = 'user' THEN
    SELECT coalesce(nullif(btrim(u.name), ''), u.email) INTO v_name FROM auth."user" u WHERE u.id = v_id::uuid;
  ELSIF v_kind = 'service' THEN
    SELECT k.name || ' (API key)' INTO v_name FROM api_keys k WHERE k.id = v_id::uuid;
  END IF;
  INSERT INTO audit_log (org_id, actor_kind, actor_id, actor_name, action, target_kind, target_id, data, request_id)
  VALUES (p_org, v_kind, v_id, coalesce(v_name, v_id), p_action, p_target_kind, p_target_id,
          coalesce(p_data, '{}'::jsonb), p_request_id);
END
$$;
-- +goose StatementEnd

CREATE OR REPLACE FUNCTION goal_invitations() RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(jsonb_agg(jsonb_build_object(
      'id', i.id, 'email', i.email, 'role', i.role,
      'factory_roles', CASE WHEN i."factoryRoles" IS JSON ARRAY THEN i."factoryRoles"::jsonb ELSE '[]'::jsonb END,
      'inviter', jsonb_build_object('id', u.id, 'name', coalesce(nullif(btrim(u.name), ''), u.email)),
      'created_at', i."createdAt", 'expires_at', i."expiresAt")
    ORDER BY i."createdAt", i.id), '[]'::jsonb)
  FROM auth.invitation i
  JOIN auth."user" u ON u.id = i."inviterId"
  JOIN organizations o ON o.id = i."organizationId" AND o.deleted_at IS NULL
  WHERE i."organizationId" = goal_org() AND i.status = 'pending' AND i."expiresAt" > now()
$$;

REVOKE ALL ON FUNCTION goal_org_live(uuid), goal_is_invitee(text, timestamptz), goal_invite_live_org() FROM PUBLIC;
REVOKE ALL ON FUNCTION goal_retire_inviter() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_org_live(uuid), goal_is_invitee(text, timestamptz) TO goal_auth;
