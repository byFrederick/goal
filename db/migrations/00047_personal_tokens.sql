-- +goose Up
-- Phase 3 (3-12b): personal tokens, a member's own credential in one organization for the CLI
-- or a script (spec 3 §4.5, §6.7). A table of its own, not a kind of API key: a key is the
-- organization's service account, a token acts as the person, with its scopes (permissions of
-- contracts/api/permissions.yaml marked pat: true) within the person's roles at each request. goald stores
-- only its hash, base64url(SHA-256(token)), and a hint (goal_pat_ and the secret's first four
-- characters); every token expires, after at most 90 days. A token lives only while its owner is
-- a member: the foreign key to auth.member cascades, so removal deletes it and a re-invitation
-- starts with none. goald creates, lists and revokes tokens as goal_app under row-level security
-- and resolves a presented one with goal_personal_token, a definer, because a token names its
-- organization before any organization is bound. Deleting an organization revokes its tokens
-- (goal_retire_org), and a revocation is announced on goal_access. Definers pin their search_path
-- as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE TABLE personal_tokens (
  id           uuid PRIMARY KEY,                                             -- UUIDv7 from goald
  org_id       uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  user_id      uuid NOT NULL,
  name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  hint         text NOT NULL CHECK (hint ~ '^goal_pat_[A-Za-z0-9_-]{4}$'),    -- shown in lists
  hash         text NOT NULL CHECK (hash ~ '^[A-Za-z0-9_-]{43}$'),            -- base64url(SHA-256(token))
  scopes       text[] NOT NULL CHECK (cardinality(scopes) BETWEEN 1 AND 64),
  created_at   timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL DEFAULT now() + interval '30 days'
               CHECK (expires_at > created_at AND expires_at <= created_at + interval '90 days'),
  revoked_at   timestamptz,
  revoked_by   text CHECK (revoked_by ~ '^(user|service|agent|system):.+$'),  -- Actor.Ref()
  last_used_at timestamptz,
  CHECK ((revoked_at IS NULL) = (revoked_by IS NULL)),
  -- Removal from the organization deletes the member's tokens; a re-invitation starts empty.
  FOREIGN KEY (org_id, user_id) REFERENCES auth.member ("organizationId", "userId") ON DELETE CASCADE
);
-- The lookup of a presented token; a live token's name among its owner's; the cascade and "my
-- tokens".
CREATE UNIQUE INDEX personal_tokens_hash_key   ON personal_tokens (hash);
CREATE UNIQUE INDEX personal_tokens_name_key   ON personal_tokens (org_id, user_id, lower(name)) WHERE revoked_at IS NULL;
CREATE INDEX        personal_tokens_member_idx ON personal_tokens (org_id, user_id);

ALTER TABLE personal_tokens ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON personal_tokens
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
-- goald creates and revokes tokens; it never changes a token's grants or deletes one (revoked
-- tokens stay listed for 30 days and go with their membership or organization). last_used_at is
-- the lookup's, a definer's, so goal_app may not set it.
GRANT SELECT, INSERT ON personal_tokens TO goal_app;
GRANT UPDATE (revoked_at, revoked_by) ON personal_tokens TO goal_app;

-- goal_personal_token is the token whose hash is p_hash while it is unrevoked, unexpired, of a
-- live organization and of a member of it, with its owner as they are now: {id, org_id, user_id,
-- name, email, token_name, scopes, expires_at, org_role, factory_roles: [{factory_id, role}]};
-- NULL otherwise, which goald answers 401 whatever the path. It never returns the hash and never
-- reports platform admin. It records the token's use at most once a minute. A hash that matches a
-- revoked or expired token writes personal_token.refused {token_id, reason} to the security log
-- (user: the owner), at most once a minute per token; an unknown hash writes nothing, so nobody
-- without a token can fill the log (goald counts those failures instead). Nothing is cached: a
-- removal, a role change, a revocation or the organization's deletion holds on the next request.
-- +goose StatementBegin
CREATE FUNCTION goal_personal_token(p_hash text) RETURNS jsonb
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  t      personal_tokens%ROWTYPE;
  v_role text;
  v_name text;
  v_mail text;
BEGIN
  SELECT pt.*, m.role, u.name, u.email INTO t.id, t.org_id, t.user_id, t.name, t.hint, t.hash, t.scopes, t.created_at,
         t.expires_at, t.revoked_at, t.revoked_by, t.last_used_at, v_role, v_name, v_mail
  FROM personal_tokens pt
  JOIN organizations o ON o.id = pt.org_id AND o.deleted_at IS NULL
  JOIN auth.member m ON m."organizationId" = pt.org_id AND m."userId" = pt.user_id
  JOIN auth."user" u ON u.id = pt.user_id
  WHERE pt.hash = p_hash AND pt.revoked_at IS NULL AND pt.expires_at > now();
  IF NOT FOUND THEN
    SELECT pt.* INTO t FROM personal_tokens pt
    WHERE pt.hash = p_hash AND (pt.revoked_at IS NOT NULL OR pt.expires_at <= now());
    IF FOUND AND NOT EXISTS (
      SELECT 1 FROM auth_events e
      WHERE e.user_id = t.user_id AND e.at > now() - interval '1 minute'
        AND e.kind = 'personal_token.refused' AND e.data ->> 'token_id' = t.id::text) THEN
      INSERT INTO auth_events (kind, user_id, data)
      VALUES ('personal_token.refused', t.user_id, jsonb_build_object(
        'token_id', t.id, 'reason', CASE WHEN t.revoked_at IS NOT NULL THEN 'revoked' ELSE 'expired' END));
    END IF;
    RETURN NULL;
  END IF;
  IF t.last_used_at IS NULL OR t.last_used_at < now() - interval '1 minute' THEN
    UPDATE personal_tokens SET last_used_at = now() WHERE id = t.id;
  END IF;
  RETURN jsonb_build_object(
    'id', t.id, 'org_id', t.org_id, 'user_id', t.user_id, 'name', v_name, 'email', v_mail, 'token_name', t.name,
    'scopes', to_jsonb(t.scopes), 'expires_at', t.expires_at, 'org_role', v_role,
    'factory_roles', coalesce((
      SELECT jsonb_agg(jsonb_build_object('factory_id', fr.factory_id, 'role', fr.role) ORDER BY fr.factory_id)
      FROM factory_roles fr WHERE fr.org_id = t.org_id AND fr.user_id = t.user_id), '[]'::jsonb));
END
$$;
-- +goose StatementEnd

-- 00046's retire trigger, now also revoking the organization's live personal tokens, stamped
-- system:org-deleted (a token of a deleted organization already fails goal_personal_token; the
-- revocation keeps it from coming back and announces it on goal_access).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION goal_retire_org() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  UPDATE auth.invitation SET status = 'canceled', "changedBy" = 'system:org-deleted', "changedAt" = clock_timestamp()
  WHERE "organizationId" = NEW.id AND status = 'pending';
  UPDATE api_keys SET revoked_at = now(), revoked_by = 'system:org-deleted'
  WHERE org_id = NEW.id AND revoked_at IS NULL;
  UPDATE personal_tokens SET revoked_at = now(), revoked_by = 'system:org-deleted'
  WHERE org_id = NEW.id AND revoked_at IS NULL;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- 00046's access notices, now with kind personal_token {org_id, user_id, token_id} when a token
-- is revoked, so the streams it holds end at once. A member's removal already sends member.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION goal_notify_access() RETURNS trigger
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
  WHEN 'api_keys' THEN
    v_notice := json_build_object('kind', 'api_key', 'org_id', NEW.org_id, 'key_id', NEW.id);
  WHEN 'personal_tokens' THEN
    v_notice := json_build_object('kind', 'personal_token', 'org_id', NEW.org_id, 'user_id', NEW.user_id,
                                  'token_id', NEW.id);
  END CASE;
  PERFORM pg_notify('goal_access', v_notice::text);
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER personal_tokens_access_revoked AFTER UPDATE OF revoked_at ON personal_tokens
  FOR EACH ROW WHEN (OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL)
  EXECUTE FUNCTION goal_notify_access();

-- 00046's startup self-check, now requiring goal_personal_token too.
CREATE OR REPLACE FUNCTION goal_identity_ready() RETURNS boolean
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
                              'goal_claim_bootstrap', 'goal_org_factories', 'goal_apply_invitation_roles',
                              'goal_api_key', 'goal_personal_token')) = 17
$$;

REVOKE ALL ON FUNCTION goal_personal_token(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_personal_token(text) TO goal_app;
