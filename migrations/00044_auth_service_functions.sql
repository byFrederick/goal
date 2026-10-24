-- +goose Up
-- Phase 3 (3-8): the definer functions the auth service calls (spec 3 §6.5). goal_auth has DML on
-- Better Auth's tables in schema auth and nothing on Goal's; these are its only way to write the
-- security log (auth_events), count recent events for the send caps and the lockout (§8.7), claim
-- the bootstrap owner, list an organization's factories for an invitation's factory roles, and
-- grant those roles once the invitation is accepted. Each one is owned by goal_owner, SECURITY
-- DEFINER with its search_path pinned (00036), static SQL, revoked from PUBLIC and granted to
-- goal_auth only. None returns a token, a code or an email. The auth service binds no
-- organization, so the organization comes from the row Better Auth is writing, never goal_org().
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- goal_auth_event appends one entry to the security log. Only the auth service's own kinds
-- (spec 3 §6.4); goald's events (platform admins, recovery) are written by its own definers, so a
-- compromised auth service cannot forge them. The user agent is cut to the column's 512
-- characters; subject is base64url(SHA-256(lower(email) or user code)), never the value itself.
-- +goose StatementBegin
CREATE FUNCTION goal_auth_event(p_kind text, p_user uuid, p_subject text, p_ip inet, p_ua text, p_data jsonb)
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  IF p_kind NOT IN ('sign_in.succeeded', 'sign_up.refused', 'user.created', 'otp.sent', 'otp.suppressed',
                    'otp.failed', 'otp.locked', 'device.requested', 'device.approved', 'device.denied',
                    'session.revoked') THEN
    RAISE EXCEPTION 'the auth service does not write % events', p_kind USING ERRCODE = 'check_violation';
  END IF;
  IF p_data IS NOT NULL AND jsonb_typeof(p_data) <> 'object' THEN
    RAISE EXCEPTION 'an auth event''s data is an object' USING ERRCODE = 'check_violation';
  END IF;
  INSERT INTO auth_events (kind, user_id, subject, ip, user_agent, data)
  VALUES (p_kind, p_user, p_subject, p_ip, left(p_ua, 512), coalesce(p_data, '{}'));
END
$$;
-- +goose StatementEnd

-- goal_auth_count counts the events of a kind for a subject since a time: codes sent in the last
-- hour (the send cap) and failed codes in the last day (the lockout). auth_events_subject_idx
-- serves it.
CREATE FUNCTION goal_auth_count(p_kind text, p_subject text, p_since timestamptz)
RETURNS integer LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT count(*)::integer FROM auth_events WHERE kind = p_kind AND subject = p_subject AND at >= p_since
$$;

-- goal_claim_bootstrap makes the bootstrap owner (spec 3 §8.7, §14): while no platform admin
-- exists, the install's earliest user becomes one, and the owner of every live organization, both
-- recorded as system:bootstrap (00043's audit trigger reads it as via bootstrap). Two sign-ins at
-- once claim it once. It is true when p_user is a platform admin afterwards (so it is idempotent, and the auth
-- service may call it on every sign-in of the bootstrap email until it holds), false when someone
-- else is, or p_user is not the earliest user. Claims and removals of platform admins are
-- serialized on the advisory lock 00043's goal_keep_a_platform_admin takes.
-- +goose StatementBegin
CREATE FUNCTION goal_claim_bootstrap(p_user uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM platform_admins) THEN
    PERFORM pg_advisory_xact_lock(hashtext('goal.platform_admins'));
  END IF;
  IF EXISTS (SELECT 1 FROM platform_admins) THEN
    RETURN EXISTS (SELECT 1 FROM platform_admins WHERE user_id = p_user);
  END IF;
  IF p_user IS DISTINCT FROM (SELECT id FROM auth."user" ORDER BY "createdAt", id LIMIT 1) THEN
    RETURN false;
  END IF;

  INSERT INTO platform_admins (user_id, added_by) VALUES (p_user, 'system:bootstrap');
  INSERT INTO auth.member (id, "organizationId", "userId", role, "createdAt", "changedBy", "changedAt")
  SELECT goal_uuidv7(), o.id, p_user, 'owner', now(), 'system:bootstrap', clock_timestamp()
  FROM organizations o JOIN auth.organization a ON a.id = o.id
  WHERE o.deleted_at IS NULL
  ON CONFLICT ("organizationId", "userId") DO UPDATE
    SET role = 'owner', "changedBy" = 'system:bootstrap', "changedAt" = clock_timestamp()
    WHERE auth.member.role <> 'owner';
  INSERT INTO auth_events (kind, user_id, data) VALUES ('platform_admin.added', p_user, '{"via": "bootstrap"}');
  RETURN true;
END
$$;
-- +goose StatementEnd

-- goal_org_factories lists a live organization's factories, by name, so the auth service can
-- check an invitation's factory roles before Better Auth stores it. A deleted or unknown
-- organization has none.
CREATE FUNCTION goal_org_factories(p_org uuid)
RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(jsonb_agg(jsonb_build_object('id', f.id, 'name', f.name) ORDER BY f.name, f.id), '[]')
  FROM factories f JOIN organizations o ON o.id = f.org_id
  WHERE f.org_id = p_org AND o.deleted_at IS NULL
$$;

-- goal_apply_invitation_roles grants an accepted invitation's factory roles to the invitee, now a
-- member: each role of a factory that still exists, granted by the inviter, with its audit entry
-- (factory_role.granted, via invitation). A role the invitee already holds is left as it is, so a
-- second call does nothing. An invitation that is not accepted, has no factory roles, or whose
-- invitee is no member does nothing either. Entries that are not a factory id and a factory role
-- are skipped (the auth service checked them when the invitation was made).
-- +goose StatementBegin
CREATE FUNCTION goal_apply_invitation_roles(p_invitation uuid)
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  v_org     uuid;
  v_email   text;
  v_roles   text;
  v_inviter uuid;
  v_user    uuid;
BEGIN
  SELECT "organizationId", email, "factoryRoles", "inviterId" INTO v_org, v_email, v_roles, v_inviter
  FROM auth.invitation WHERE id = p_invitation AND status = 'accepted';
  IF NOT FOUND OR v_roles IS NULL THEN
    RETURN;
  END IF;
  SELECT m."userId" INTO v_user
  FROM auth.member m JOIN auth."user" u ON u.id = m."userId"
  WHERE m."organizationId" = v_org AND lower(u.email) = lower(v_email);
  IF NOT FOUND THEN
    RETURN;
  END IF;

  WITH wanted AS (
    SELECT DISTINCT ON (r.factory_id) r.factory_id, r.role
    FROM (
      SELECT CASE WHEN e->>'factoryId' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                  THEN (e->>'factoryId')::uuid END AS factory_id,
             e->>'role' AS role
      FROM jsonb_array_elements(CASE WHEN jsonb_typeof(v_roles::jsonb) = 'array' THEN v_roles::jsonb ELSE '[]' END) e
    ) r
    WHERE r.factory_id IS NOT NULL AND r.role IN ('viewer', 'approver', 'editor')
    ORDER BY r.factory_id
  ), granted AS (
    INSERT INTO factory_roles (org_id, factory_id, user_id, role, granted_by)
    SELECT v_org, f.id, v_user, w.role, 'user:' || v_inviter
    FROM wanted w JOIN factories f ON f.org_id = v_org AND f.id = w.factory_id
    ON CONFLICT DO NOTHING
    RETURNING factory_id, role
  )
  INSERT INTO audit_log (org_id, actor_kind, actor_id, actor_name, action, target_kind, target_id, factory_id, data)
  SELECT v_org, 'user', v_inviter::text, coalesce((SELECT name FROM auth."user" WHERE id = v_inviter), ''),
         'factory_role.granted', 'member', v_user::text, g.factory_id,
         jsonb_build_object('user_id', v_user, 'factory_id', g.factory_id, 'to', g.role, 'via', 'invitation')
  FROM granted g;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION goal_auth_event(text, uuid, text, inet, text, jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION goal_auth_count(text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION goal_claim_bootstrap(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION goal_org_factories(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION goal_apply_invitation_roles(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_auth_event(text, uuid, text, inet, text, jsonb) TO goal_auth;
GRANT EXECUTE ON FUNCTION goal_auth_count(text, text, timestamptz) TO goal_auth;
GRANT EXECUTE ON FUNCTION goal_claim_bootstrap(uuid) TO goal_auth;
GRANT EXECUTE ON FUNCTION goal_org_factories(uuid) TO goal_auth;
GRANT EXECUTE ON FUNCTION goal_apply_invitation_roles(uuid) TO goal_auth;
