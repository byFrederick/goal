-- +goose Up
-- Phase 3 (3-12): API keys, an organization's service accounts (spec 3 §4.3, §6.6). A key has a
-- name, scopes (permissions of api/permissions.yaml marked keys: true), an optional factory
-- restriction, a role ceiling and a mandatory expiry of at most 90 days; goald stores only its
-- hash, base64url(SHA-256(key)), and a hint (goal_sk_ and the secret's first four characters).
-- Both tables are org tables under the tenancy kit; goald creates, lists and revokes keys as
-- goal_app under row-level security and resolves a presented key with goal_api_key, a definer,
-- because a key names its organization before any organization is bound. Deleting an
-- organization revokes its keys (goal_retire_org), a revocation is announced on goal_access, and
-- the audit log names a key's actions "<key name> (API key)". Personal tokens (3-12b) are a
-- table of their own (00047), not a kind of key. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE TABLE api_keys (
  id            uuid PRIMARY KEY,                                            -- UUIDv7 from goald
  org_id        uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  name          text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  hint          text NOT NULL CHECK (hint ~ '^goal_sk_[A-Za-z0-9_-]{4}$'),    -- shown in lists
  hash          text NOT NULL CHECK (hash ~ '^[A-Za-z0-9_-]{43}$'),           -- base64url(SHA-256(key))
  scopes        text[] NOT NULL CHECK (cardinality(scopes) BETWEEN 1 AND 64),
  role_ceiling  text NOT NULL CHECK (role_ceiling IN ('viewer', 'approver', 'editor')),
  all_factories boolean NOT NULL,                                            -- false: api_key_factories
  created_by    text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),   -- Actor.Ref()
  created_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL
                CHECK (expires_at > created_at AND expires_at <= created_at + interval '90 days'),
  revoked_at    timestamptz,
  revoked_by    text CHECK (revoked_by ~ '^(user|service|agent|system):.+$'),
  last_used_at  timestamptz,
  CHECK ((revoked_at IS NULL) = (revoked_by IS NULL)),
  UNIQUE (org_id, id)
);
-- The lookup of a presented key, and its name among the organization's live keys.
CREATE UNIQUE INDEX api_keys_hash_key ON api_keys (hash);
CREATE UNIQUE INDEX api_keys_name_key ON api_keys (org_id, lower(name)) WHERE revoked_at IS NULL;

-- The factories a restricted key may reach; a key goes with its organization, a row with its
-- factory.
CREATE TABLE api_key_factories (
  org_id     uuid NOT NULL DEFAULT goal_org(),
  key_id     uuid NOT NULL,
  factory_id uuid NOT NULL,
  PRIMARY KEY (org_id, key_id, factory_id),
  FOREIGN KEY (org_id, key_id)     REFERENCES api_keys (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);
CREATE INDEX api_key_factories_factory_idx ON api_key_factories (org_id, factory_id);   -- the factory's cascade

ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON api_keys
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE api_key_factories ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON api_key_factories
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
-- goald creates and revokes keys; it never changes a key's grants or deletes one (revoked keys
-- stay listed for 30 days and go with their organization's purge).
GRANT SELECT, INSERT ON api_keys, api_key_factories TO goal_app;
GRANT UPDATE (revoked_at, revoked_by) ON api_keys TO goal_app;

-- goal_api_key is the key whose hash is p_hash while it is unrevoked, unexpired and of a live
-- organization: {id, org_id, name, scopes, role_ceiling, factories (null: every factory),
-- expires_at}; NULL otherwise, which goald answers 401 whatever the path. It never returns the
-- hash. It records the key's use at most once a minute, so a busy key does not write on every
-- request. Nothing is cached: a revocation or the organization's deletion holds on the next
-- request.
-- +goose StatementBegin
CREATE FUNCTION goal_api_key(p_hash text) RETURNS jsonb
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
DECLARE
  k api_keys%ROWTYPE;
BEGIN
  SELECT ak.* INTO k
  FROM api_keys ak JOIN organizations o ON o.id = ak.org_id AND o.deleted_at IS NULL
  WHERE ak.hash = p_hash AND ak.revoked_at IS NULL AND ak.expires_at > now();
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  IF k.last_used_at IS NULL OR k.last_used_at < now() - interval '1 minute' THEN
    UPDATE api_keys SET last_used_at = now() WHERE id = k.id;
  END IF;
  RETURN jsonb_build_object(
    'id', k.id, 'org_id', k.org_id, 'name', k.name, 'scopes', to_jsonb(k.scopes), 'role_ceiling', k.role_ceiling,
    'factories', CASE WHEN k.all_factories THEN NULL ELSE coalesce((
      SELECT jsonb_agg(f.factory_id ORDER BY f.factory_id) FROM api_key_factories f
      WHERE f.org_id = k.org_id AND f.key_id = k.id), '[]'::jsonb) END,
    'expires_at', k.expires_at);
END
$$;
-- +goose StatementEnd

-- 00042's audit writer, now naming API keys: a service actor's name is "<key name> (API key)",
-- as goald records a key's own writes (domain.Principal.Actor).
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
    SELECT u.name INTO v_name FROM auth."user" u WHERE u.id = v_id::uuid;
  ELSIF v_kind = 'service' THEN
    SELECT k.name || ' (API key)' INTO v_name FROM api_keys k WHERE k.id = v_id::uuid;
  END IF;
  INSERT INTO audit_log (org_id, actor_kind, actor_id, actor_name, action, target_kind, target_id, data, request_id)
  VALUES (p_org, v_kind, v_id, coalesce(v_name, v_id), p_action, p_target_kind, p_target_id,
          coalesce(p_data, '{}'::jsonb), p_request_id);
END
$$;
-- +goose StatementEnd

-- 00042's retire trigger, now also revoking the organization's live keys, stamped
-- system:org-deleted (a key of a deleted organization already fails goal_api_key; the revocation
-- keeps it from coming back and announces it on goal_access).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION goal_retire_org() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  UPDATE auth.invitation SET status = 'canceled', "changedBy" = 'system:org-deleted', "changedAt" = clock_timestamp()
  WHERE "organizationId" = NEW.id AND status = 'pending';
  UPDATE api_keys SET revoked_at = now(), revoked_by = 'system:org-deleted'
  WHERE org_id = NEW.id AND revoked_at IS NULL;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- 00043's access notices, now with kind api_key {org_id, key_id} when a key is revoked, so the
-- streams it holds end at once.
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
  END CASE;
  PERFORM pg_notify('goal_access', v_notice::text);
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER api_keys_access_revoked AFTER UPDATE OF revoked_at ON api_keys
  FOR EACH ROW WHEN (OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL)
  EXECUTE FUNCTION goal_notify_access();

-- 00045's startup self-check, now requiring goal_api_key too.
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
                              'goal_api_key')) = 16
$$;

REVOKE ALL ON FUNCTION goal_api_key(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_api_key(text) TO goal_app;
