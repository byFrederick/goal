-- +goose Up
-- Phase 4 (4-8): connection kinds and connections (spec 4 §4.2, §4.3, §6). A kind is a template:
-- the platform's ship with goald (org_id NULL, written only through goal_sync_platform_kinds at
-- goald's start), an organization's own are written by goald as goal_app under row-level security
-- and hide a platform kind of the same name inside that organization. A connection is an org row:
-- its kind, its non-secret field values and, when it has any, its secret values sealed as one JSON
-- object with AES-256-GCM under the platform master key (key_id, nonce, ciphertext; the associated
-- data binds organization, connection and key id, so a ciphertext copied to another row does not
-- open). No column holds a secret in the clear. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE TABLE connection_kinds (
  id          uuid PRIMARY KEY,                                            -- UUIDv7 from goald
  org_id      uuid DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,   -- NULL: the platform's
  name        text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  spec        jsonb NOT NULL CHECK (jsonb_typeof(spec) = 'object' AND octet_length(spec::text) <= 65536),
  spec_sha256 text NOT NULL CHECK (spec_sha256 ~ '^[0-9a-f]{64}$'),
  created_by  text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),     -- Actor.Ref()
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE NULLS NOT DISTINCT (org_id, name)
);

CREATE TABLE connections (
  id              uuid PRIMARY KEY,                                        -- UUIDv7 from goald
  org_id          uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  name            text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  -- NO ACTION, checked at the statement's end: an organization's purge removes its kinds and
  -- connections in one cascade in whatever order; alone, a kind in use cannot be deleted.
  kind_id         uuid NOT NULL REFERENCES connection_kinds (id),
  description     text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
  fields          jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(fields) = 'object' AND octet_length(fields::text) <= 8192),
  secret_fields   text[] NOT NULL DEFAULT '{}',                            -- the names that are set, for display
  key_id          text,
  nonce           bytea CHECK (octet_length(nonce) = 12),
  ciphertext      bytea CHECK (octet_length(ciphertext) <= 65536),
  CHECK ((key_id IS NULL) = (nonce IS NULL) AND (nonce IS NULL) = (ciphertext IS NULL)),
  CHECK ((ciphertext IS NULL) = (cardinality(secret_fields) = 0)),
  created_by      text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_by      text NOT NULL CHECK (updated_by ~ '^(user|service|agent|system):.+$'),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  last_used_at    timestamptz,
  last_test_at    timestamptz,
  last_test_ok    boolean,
  last_test_error text CHECK (length(last_test_error) <= 500),
  CHECK ((last_test_at IS NULL) = (last_test_ok IS NULL)),
  UNIQUE (org_id, name),
  UNIQUE (org_id, id)
);
CREATE INDEX connections_kind_idx ON connections (kind_id);   -- a kind's deletion checks its connections

-- A connection's kind is the platform's or its own organization's: a foreign key ignores
-- row-level security, so without this a connection could take another organization's kind.
-- +goose StatementBegin
CREATE FUNCTION goal_connection_kind_of_org() RETURNS trigger
LANGUAGE plpgsql SET search_path FROM CURRENT
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM connection_kinds k
                 WHERE k.id = NEW.kind_id AND (k.org_id IS NULL OR k.org_id = NEW.org_id)) THEN
    RAISE EXCEPTION 'connection kind % is neither the platform''s nor the organization''s', NEW.kind_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'connections_kind_of_org';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER connections_kind_of_org BEFORE INSERT OR UPDATE OF kind_id, org_id ON connections
  FOR EACH ROW EXECUTE FUNCTION goal_connection_kind_of_org();

-- Every organization reads the platform's kinds; it writes only its own. Without an organization bound
-- nothing reads even those, as with every org table (TestRLS_RawSQLWithoutOrgFails).
ALTER TABLE connection_kinds ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON connection_kinds
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
CREATE POLICY platform_kinds ON connection_kinds FOR SELECT USING (org_id IS NULL AND (SELECT goal_org()) IS NOT NULL);
ALTER TABLE connections ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON connections
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON connection_kinds, connections TO goal_app;
GRANT UPDATE (spec, spec_sha256, updated_at) ON connection_kinds TO goal_app;
GRANT UPDATE (description, fields, secret_fields, key_id, nonce, ciphertext, updated_by, updated_at, last_used_at,
              last_test_at, last_test_ok, last_test_error) ON connections TO goal_app;

-- goal_sync_platform_kinds makes p_kinds, [{id, name, spec, spec_sha256}], the platform's kinds:
-- new ones are added with their id, changed ones (another spec_sha256) replaced, and ones no
-- longer listed deleted unless a connection uses them. goald calls it at its start, in no
-- organization; instances starting together take turns.
-- +goose StatementBegin
CREATE FUNCTION goal_sync_platform_kinds(p_kinds jsonb) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
BEGIN
  IF jsonb_typeof(p_kinds) IS DISTINCT FROM 'array' THEN
    RAISE EXCEPTION 'platform kinds are a JSON array' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  PERFORM pg_advisory_xact_lock(hashtext('goal.platform_kinds'));
  INSERT INTO connection_kinds AS ck (id, org_id, name, spec, spec_sha256, created_by)
  SELECT (k->>'id')::uuid, NULL, k->>'name', k->'spec', k->>'spec_sha256', 'system:goald'
  FROM jsonb_array_elements(p_kinds) k
  ON CONFLICT (org_id, name) DO UPDATE SET spec = EXCLUDED.spec, spec_sha256 = EXCLUDED.spec_sha256, updated_at = now()
    WHERE ck.spec_sha256 <> EXCLUDED.spec_sha256;
  DELETE FROM connection_kinds ck
  WHERE ck.org_id IS NULL
    AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(p_kinds) k WHERE k->>'name' = ck.name)
    AND NOT EXISTS (SELECT 1 FROM connections c WHERE c.kind_id = ck.id);
END
$$;
-- +goose StatementEnd

-- goal_connection_key_ids are the master key ids some connection is sealed under, in every
-- organization: goald refuses to start without each of them (spec 4 §5.2). Ids only, never a
-- sealed value.
CREATE FUNCTION goal_connection_key_ids() RETURNS text[]
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(array_agg(DISTINCT key_id ORDER BY key_id), '{}') FROM connections WHERE key_id IS NOT NULL
$$;

REVOKE ALL ON FUNCTION goal_sync_platform_kinds(jsonb), goal_connection_key_ids(), goal_connection_kind_of_org() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_sync_platform_kinds(jsonb), goal_connection_key_ids() TO goal_app;
