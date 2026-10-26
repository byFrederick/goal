-- +goose Up
-- Phase 3 (3-9): the definer functions goald calls (spec 3 §6.5 "app"). goal_app has no USAGE on
-- schema auth, so these are its only way to read Better Auth's rows: who a session token is, with
-- the person's role and factory roles in the organization of the request (goal_principal), the
-- organizations a person belongs to (goal_orgs_for), the members and pending invitations of the
-- transaction's organization (goal_members, goal_invitations), a pending CLI sign-in
-- (goal_device_request), the platform admins and the security log, the clean-up job, and the
-- startup self-check (goal_identity_ready). Each one is owned by goal_owner, SECURITY DEFINER with
-- its search_path pinned (00036), static SQL, revoked from PUBLIC and granted to goal_app only.
-- None returns a token, a code, a hash or a secret. Functions that act on an organization use
-- goal_org(), the caller's transaction organization; only the principal lookups, which run before
-- an organization is bound, take one. Results are jsonb, which the adapter decodes (sqlc cannot
-- type RETURNS TABLE of a definer's composite, A §6.4).
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- goal_principal is who the session token p_token signs in, while the session has not expired:
-- the user, the session's dates, whether they are a platform admin, and their role and factory
-- roles in p_org. org_role is null (and factory_roles empty) unless p_org is a live organization
-- the user is a member of. NULL for an unknown or expired token, which goald answers 401 whatever
-- the path. auth.session's unique token index serves it; nothing is cached, so a removal, a role
-- change or a sign-out holds on the next request.
CREATE FUNCTION goal_principal(p_token text, p_org uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT jsonb_build_object(
    'user_id', u.id, 'name', u.name, 'email', u.email,
    'session_id', s.id, 'created_at', s."createdAt", 'expires_at', s."expiresAt",
    'platform_admin', EXISTS (SELECT 1 FROM platform_admins pa WHERE pa.user_id = u.id),
    'org_role', m.role,
    'factory_roles', coalesce((
      SELECT jsonb_agg(jsonb_build_object('factory_id', fr.factory_id, 'role', fr.role) ORDER BY fr.factory_id)
      FROM factory_roles fr WHERE m.role IS NOT NULL AND fr.org_id = m."organizationId" AND fr.user_id = u.id), '[]'::jsonb))
  FROM auth.session s
  JOIN auth."user" u ON u.id = s."userId"
  LEFT JOIN organizations o ON o.id = p_org AND o.deleted_at IS NULL
  LEFT JOIN auth.member m ON m."organizationId" = o.id AND m."userId" = u.id
  WHERE s.token = p_token AND s."expiresAt" > now()
$$;

-- goal_orgs_for lists the live organizations p_user belongs to, by name, with their role there and
-- the factories they can see: all of them for owners and admins, those they hold a factory role on
-- for members.
CREATE FUNCTION goal_orgs_for(p_user uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(jsonb_agg(jsonb_build_object(
      'id', o.id, 'slug', o.slug, 'name', o.name, 'mark', o.mark, 'max_slots', o.max_slots, 'created_at', o.created_at,
      'role', m.role,
      'factory_count', CASE WHEN m.role IN ('owner', 'admin')
                            THEN (SELECT count(*) FROM factories f WHERE f.org_id = o.id)
                            ELSE (SELECT count(*) FROM factory_roles fr WHERE fr.org_id = o.id AND fr.user_id = p_user) END)
    ORDER BY o.name, o.id), '[]'::jsonb)
  FROM auth.member m
  JOIN organizations o ON o.id = m."organizationId" AND o.deleted_at IS NULL
  WHERE m."userId" = p_user
$$;

-- goal_members lists the members of the transaction's organization, by name: their membership,
-- role, when they joined, when they were last active (their newest session's update, which Better
-- Auth bumps as it extends the session) and their factory roles. Emails go only to the
-- organization's own members (member:read).
CREATE FUNCTION goal_members() RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(jsonb_agg(jsonb_build_object(
      'member_id', m.id, 'user_id', u.id, 'name', u.name, 'email', u.email, 'role', m.role, 'joined_at', m."createdAt",
      'last_active_at', (SELECT max(s."updatedAt") FROM auth.session s WHERE s."userId" = u.id),
      'factory_roles', coalesce((
        SELECT jsonb_agg(jsonb_build_object('factory_id', fr.factory_id, 'role', fr.role) ORDER BY fr.factory_id)
        FROM factory_roles fr WHERE fr.org_id = m."organizationId" AND fr.user_id = u.id), '[]'::jsonb))
    ORDER BY u.name, u.id), '[]'::jsonb)
  FROM auth.member m
  JOIN auth."user" u ON u.id = m."userId"
  JOIN organizations o ON o.id = m."organizationId" AND o.deleted_at IS NULL
  WHERE m."organizationId" = goal_org()
$$;

-- goal_invitations lists the pending, unexpired invitations of the transaction's organization,
-- oldest first, with their factory roles (the JSON Better Auth keeps as text; anything else reads
-- as none) and who invited.
CREATE FUNCTION goal_invitations() RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(jsonb_agg(jsonb_build_object(
      'id', i.id, 'email', i.email, 'role', i.role,
      'factory_roles', CASE WHEN i."factoryRoles" IS JSON ARRAY THEN i."factoryRoles"::jsonb ELSE '[]'::jsonb END,
      'inviter', jsonb_build_object('id', u.id, 'name', u.name),
      'created_at', i."createdAt", 'expires_at', i."expiresAt")
    ORDER BY i."createdAt", i.id), '[]'::jsonb)
  FROM auth.invitation i
  JOIN auth."user" u ON u.id = i."inviterId"
  JOIN organizations o ON o.id = i."organizationId" AND o.deleted_at IS NULL
  WHERE i."organizationId" = goal_org() AND i.status = 'pending' AND i."expiresAt" > now()
$$;

-- goal_user_code_subject is how the auth service names a device grant's user code in the security
-- log (auth/src/goal.ts userCodeSubject): base64url, unpadded, of the SHA-256 of the code's letters
-- and digits in upper case. Not a definer; nobody but the definers here calls it.
CREATE FUNCTION goal_user_code_subject(p_user_code text) RETURNS text
LANGUAGE sql IMMUTABLE SET search_path FROM CURRENT
AS $$
  SELECT rtrim(translate(encode(sha256(convert_to(upper(regexp_replace(p_user_code, '[^a-zA-Z0-9]', '', 'g')), 'UTF8')),
                                'base64'), '+/', '-_'), '=')
$$;

-- goal_device_request describes a pending, unexpired CLI sign-in by the user code the person
-- typed (dashes and case ignored): the client, when it was asked for and until when, and the
-- address and user agent its device.requested event recorded (3-8's after hook). NULL otherwise.
CREATE FUNCTION goal_device_request(p_user_code text) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT jsonb_build_object(
    'client_id', d."clientId", 'status', d.status, 'expires_at', d."expiresAt",
    'requested_at', e.at, 'ip', host(e.ip), 'user_agent', e.user_agent)
  FROM auth."deviceCode" d
  LEFT JOIN LATERAL (
    SELECT a.at, a.ip, a.user_agent FROM auth_events a
    WHERE a.kind = 'device.requested' AND a.subject = goal_user_code_subject(p_user_code)
    ORDER BY a.at DESC LIMIT 1) e ON true
  WHERE upper(regexp_replace(d."userCode", '[^a-zA-Z0-9]', '', 'g')) = upper(regexp_replace(p_user_code, '[^a-zA-Z0-9]', '', 'g'))
    AND d.status = 'pending' AND d."expiresAt" > now()
$$;

-- goal_platform_admins lists the install's platform admins, earliest first.
CREATE FUNCTION goal_platform_admins() RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(jsonb_agg(jsonb_build_object(
      'user_id', u.id, 'name', u.name, 'email', u.email, 'added_by', pa.added_by, 'added_at', pa.added_at)
    ORDER BY pa.added_at, pa.user_id), '[]'::jsonb)
  FROM platform_admins pa JOIN auth."user" u ON u.id = pa.user_id
$$;

-- goal_add_platform_admin makes the user with email p_email (case-insensitive) a platform admin,
-- by p_by (an actor ref), and records platform_admin.added; adding one twice changes nothing.
-- It returns the user ({id, name, email}), or NULL when no user has that email.
-- +goose StatementBegin
CREATE FUNCTION goal_add_platform_admin(p_email text, p_by text) RETURNS jsonb
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  u auth."user";
BEGIN
  SELECT * INTO u FROM auth."user" WHERE lower(email) = lower(trim(p_email));
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  PERFORM pg_advisory_xact_lock(hashtext('goal.platform_admins'));
  INSERT INTO platform_admins (user_id, added_by) VALUES (u.id, p_by) ON CONFLICT (user_id) DO NOTHING;
  IF FOUND THEN
    INSERT INTO auth_events (kind, user_id, data) VALUES ('platform_admin.added', u.id, jsonb_build_object('by', p_by));
  END IF;
  RETURN jsonb_build_object('id', u.id, 'name', u.name, 'email', u.email);
END
$$;
-- +goose StatementEnd

-- goal_remove_platform_admin removes p_user from the platform admins, by p_by, and records
-- platform_admin.removed. no_data_found when they are not one; the last one stays
-- (platform_admins_keep_one, 00043, check_violation at commit).
-- +goose StatementBegin
CREATE FUNCTION goal_remove_platform_admin(p_user uuid, p_by text) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  DELETE FROM platform_admins WHERE user_id = p_user;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'user % is not a platform admin', p_user USING ERRCODE = 'no_data_found';
  END IF;
  INSERT INTO auth_events (kind, user_id, data) VALUES ('platform_admin.removed', p_user, jsonb_build_object('by', p_by));
END
$$;
-- +goose StatementEnd

-- goal_auth_events is one page of the security log, newest first: up to p_limit (1 to 500) events
-- older than id p_before (NULL: the newest), of kind p_kind when it is set. Subjects are hashes.
CREATE FUNCTION goal_auth_events(p_before bigint, p_limit integer, p_kind text) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(jsonb_agg(jsonb_build_object(
      'id', e.id, 'at', e.at, 'kind', e.kind, 'user_id', e.user_id, 'subject', e.subject, 'ip', host(e.ip),
      'user_agent', e.user_agent, 'data', e.data) ORDER BY e.id DESC), '[]'::jsonb)
  FROM (SELECT * FROM auth_events a
        WHERE (p_before IS NULL OR a.id < p_before) AND (p_kind IS NULL OR a.kind = p_kind)
        ORDER BY a.id DESC LIMIT greatest(1, least(coalesce(p_limit, 100), 500))) e
$$;

-- goal_auth_cleanup deletes what sign-in no longer needs, as of p_now: sessions expired over a day
-- ago (their access notices are sent), expired verification values and device codes, rate-limit
-- counters idle for a day, and security-log events older than 90 days. It returns the counts.
-- +goose StatementBegin
CREATE FUNCTION goal_auth_cleanup(p_now timestamptz) RETURNS jsonb
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  v_sessions bigint;
  v_verifications bigint;
  v_devices bigint;
  v_limits bigint;
  v_events bigint;
BEGIN
  DELETE FROM auth.session WHERE "expiresAt" < p_now - interval '1 day';
  GET DIAGNOSTICS v_sessions = ROW_COUNT;
  DELETE FROM auth.verification WHERE "expiresAt" < p_now;
  GET DIAGNOSTICS v_verifications = ROW_COUNT;
  DELETE FROM auth."deviceCode" WHERE "expiresAt" < p_now;
  GET DIAGNOSTICS v_devices = ROW_COUNT;
  -- lastRequest is Better Auth's epoch milliseconds.
  DELETE FROM auth."rateLimit" WHERE "lastRequest" < (extract(epoch FROM p_now - interval '1 day') * 1000)::bigint;
  GET DIAGNOSTICS v_limits = ROW_COUNT;
  DELETE FROM auth_events WHERE at < p_now - interval '90 days';
  GET DIAGNOSTICS v_events = ROW_COUNT;
  RETURN jsonb_build_object('sessions', v_sessions, 'verifications', v_verifications, 'device_codes', v_devices,
                            'rate_limits', v_limits, 'auth_events', v_events);
END
$$;
-- +goose StatementEnd

-- goal_identity_ready is goald's startup self-check under GOAL_AUTH=session (spec 3 §8.3): Better
-- Auth's tables and phase 3's definers exist, and goal_app still cannot use schema auth.
CREATE FUNCTION goal_identity_ready() RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT to_regclass('auth.session') IS NOT NULL AND to_regclass('auth."user"') IS NOT NULL
     AND to_regclass('auth.member') IS NOT NULL AND to_regclass('auth.invitation') IS NOT NULL
     AND to_regclass('auth."deviceCode"') IS NOT NULL
     AND NOT has_schema_privilege('goal_app', 'auth', 'USAGE')
     AND (SELECT count(DISTINCT p.proname) FROM pg_proc p
          WHERE p.pronamespace = current_schema()::regnamespace AND p.prosecdef
            AND p.proname IN ('goal_principal', 'goal_orgs_for', 'goal_members', 'goal_invitations', 'goal_device_request',
                              'goal_platform_admins', 'goal_add_platform_admin', 'goal_remove_platform_admin',
                              'goal_auth_events', 'goal_auth_cleanup', 'goal_auth_event', 'goal_auth_count',
                              'goal_claim_bootstrap', 'goal_org_factories', 'goal_apply_invitation_roles')) = 15
$$;

REVOKE ALL ON FUNCTION goal_principal(text, uuid), goal_orgs_for(uuid), goal_members(), goal_invitations(),
  goal_user_code_subject(text), goal_device_request(text), goal_platform_admins(), goal_add_platform_admin(text, text),
  goal_remove_platform_admin(uuid, text), goal_auth_events(bigint, integer, text), goal_auth_cleanup(timestamptz),
  goal_identity_ready() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_principal(text, uuid), goal_orgs_for(uuid), goal_members(), goal_invitations(),
  goal_device_request(text), goal_platform_admins(), goal_add_platform_admin(text, text),
  goal_remove_platform_admin(uuid, text), goal_auth_events(bigint, integer, text), goal_auth_cleanup(timestamptz),
  goal_identity_ready() TO goal_app;
