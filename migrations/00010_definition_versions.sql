-- +goose Up
-- Phase 2.2 B3: versioned factory definitions (spec 2.2-B §6.1, ADR 0005 amendment). File content
-- lives in the bucket as immutable blobs (orgs/<org-id>/blobs/<sha256>); this schema indexes the
-- blobs each organization stored and keeps every draft and published version as a manifest
-- (path -> sha256), with each factory's current-version pointer. The tenancy kit of 00005/00006
-- applies: org_id defaults to goal_org(), composite keys keep rows inside one organization, and
-- every table has the org_isolation policy.

-- The blobs an organization stored. A row is written after its object is in the bucket, so a row
-- always names content that exists; manifests may only name hashes that have a row.
CREATE TABLE definition_blobs (
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  sha256     text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  size       integer NOT NULL CHECK (size BETWEEN 0 AND 1048576),   -- domain.MaxBlobSize
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, sha256)
);

-- One row per draft, published or archived (discarded draft) version of a factory definition.
-- Who did something is an Actor.Ref() (user:local, agent:<name>, system:<job>); there is no users
-- table in 2.2.
CREATE TABLE definition_versions (
  id              uuid PRIMARY KEY,
  org_id          uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id      uuid NOT NULL,
  state           text NOT NULL CHECK (state IN ('draft', 'published', 'archived')),
  number          integer CHECK (number > 0),
  base_version_id uuid,
  revision        bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  manifest        jsonb NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
  message         text NOT NULL DEFAULT '' CHECK (length(message) <= 200),
  source          jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(source) = 'object'),
  resolved        jsonb NOT NULL DEFAULT '{}',   -- phase 3's pinned connection bindings, set at publish
  created_by      text NOT NULL,
  updated_by      text NOT NULL,
  published_by    text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  published_at    timestamptz,
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  UNIQUE (factory_id, id),
  -- A draft's base is a version of the same factory. Bases are published, so they are only ever
  -- deleted with every version of their factory, in one statement (goal_purge_org).
  FOREIGN KEY (factory_id, base_version_id) REFERENCES definition_versions (factory_id, id),
  -- Gapless per factory: numbers come from factories.last_version_number under the factory's lock.
  -- This index also serves version history, newest first.
  UNIQUE (factory_id, number),
  CHECK ((state = 'published') = (number IS NOT NULL AND published_at IS NOT NULL AND published_by IS NOT NULL))
);
-- At most one open draft per factory.
CREATE UNIQUE INDEX definition_versions_one_draft ON definition_versions (factory_id) WHERE state = 'draft';

ALTER TABLE factories
  ADD COLUMN current_version_id  uuid,
  ADD COLUMN last_version_number integer NOT NULL DEFAULT 0 CHECK (last_version_number >= 0),
  ADD COLUMN paused_at           timestamptz,
  ADD COLUMN paused_by           text,
  ADD COLUMN created_by          text,   -- NULL for a factory registered before versions existed
  ADD CONSTRAINT factories_current_version_fkey
    FOREIGN KEY (id, current_version_id) REFERENCES definition_versions (factory_id, id);
-- The definition now lives in versions; the parsed copy stays for 2.2-E's backfill and is dropped
-- in 2.3.
ALTER TABLE factories ALTER COLUMN definition DROP NOT NULL, ALTER COLUMN definition_sha256 DROP NOT NULL;

-- Published and archived versions never change; a draft only becomes published or archived, and
-- no version moves to another factory or organization.
-- +goose StatementBegin
CREATE FUNCTION definition_versions_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.state <> 'draft' THEN
    RAISE EXCEPTION 'definition version % is %, not editable', OLD.id, OLD.state USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.factory_id <> OLD.factory_id OR NEW.org_id <> OLD.org_id THEN
    RAISE EXCEPTION 'definition version % cannot move', OLD.id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER definition_versions_guard BEFORE UPDATE ON definition_versions
  FOR EACH ROW EXECUTE FUNCTION definition_versions_guard();

-- Published versions are never deleted: deleting a never-published factory cascades to its drafts
-- only. The one exception is goal_purge_org() hard-deleting an organization that was deleted.
-- +goose StatementBegin
CREATE FUNCTION definition_versions_keep_published() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.state = 'published'
     AND NOT EXISTS (SELECT 1 FROM organizations WHERE id = OLD.org_id AND deleted_at IS NOT NULL) THEN
    RAISE EXCEPTION 'published definition version % cannot be deleted', OLD.id USING ERRCODE = 'check_violation';
  END IF;
  RETURN OLD;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER definition_versions_keep_published BEFORE DELETE ON definition_versions
  FOR EACH ROW EXECUTE FUNCTION definition_versions_keep_published();

-- The pointer names only a published version (the foreign key covers "of the same factory").
-- +goose StatementBegin
CREATE FUNCTION factories_current_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.current_version_id IS NOT NULL AND NEW.current_version_id IS DISTINCT FROM OLD.current_version_id
     AND NOT EXISTS (SELECT 1 FROM definition_versions WHERE id = NEW.current_version_id AND state = 'published') THEN
    RAISE EXCEPTION 'current version % of factory % is not published', NEW.current_version_id, NEW.id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER factories_current_guard BEFORE UPDATE OF current_version_id ON factories
  FOR EACH ROW EXECUTE FUNCTION factories_current_guard();

-- Realtime: every goald LISTENs on goal_definitions (postgres.DefinitionNotices) and reloads when
-- a factory's current version moves; editors see draft saves. Payloads carry ids, never content,
-- and always the organization. Notifications are sent on commit, in order.

-- A factory created or deleted ('factory'), or its current version or pause moved ('current',
-- with the current version's id and number).
-- +goose StatementBegin
CREATE FUNCTION notify_factory_definition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  current_number integer;
BEGIN
  IF TG_OP = 'DELETE' THEN
    PERFORM pg_notify('goal_definitions', json_build_object(
      'org_id', OLD.org_id, 'factory_id', OLD.id, 'kind', 'factory')::text);
    RETURN OLD;
  END IF;
  IF TG_OP = 'INSERT' THEN
    PERFORM pg_notify('goal_definitions', json_build_object(
      'org_id', NEW.org_id, 'factory_id', NEW.id, 'kind', 'factory')::text);
    RETURN NEW;
  END IF;
  IF NEW.current_version_id IS NOT DISTINCT FROM OLD.current_version_id
     AND NEW.paused_at IS NOT DISTINCT FROM OLD.paused_at THEN
    RETURN NEW;
  END IF;
  SELECT number INTO current_number FROM definition_versions WHERE id = NEW.current_version_id;
  PERFORM pg_notify('goal_definitions', json_build_object(
    'org_id', NEW.org_id, 'factory_id', NEW.id, 'kind', 'current',
    'version_id', NEW.current_version_id, 'number', current_number)::text);
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER factories_notify_definition AFTER INSERT OR DELETE OR UPDATE OF current_version_id, paused_at ON factories
  FOR EACH ROW EXECUTE FUNCTION notify_factory_definition();

-- A version row written: a draft opened, saved, discarded or published ('draft').
-- +goose StatementBegin
CREATE FUNCTION notify_definition_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('goal_definitions', json_build_object(
    'org_id', NEW.org_id, 'factory_id', NEW.factory_id, 'kind', 'draft', 'version_id', NEW.id,
    'number', NEW.number, 'revision', NEW.revision, 'by', NEW.updated_by)::text);
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER definition_versions_notify AFTER INSERT OR UPDATE ON definition_versions
  FOR EACH ROW EXECUTE FUNCTION notify_definition_version();

-- A factory with its current version and open draft, as ports.FactoryRecord lists it. Runs with
-- the caller's rights, so row-level security applies through it.
CREATE VIEW factory_records WITH (security_invoker = true) AS
SELECT f.id, f.org_id, f.name, f.title, f.last_version_number, f.paused_at, f.paused_by, f.created_at, f.created_by,
       c.id AS current_id, c.number AS current_number, c.revision AS current_revision,
       c.published_at AS current_at, c.published_by AS current_by,
       d.id AS draft_id, d.revision AS draft_revision, d.updated_at AS draft_at, d.updated_by AS draft_by
FROM factories f
LEFT JOIN definition_versions c ON c.id = f.current_version_id
LEFT JOIN definition_versions d ON d.factory_id = f.id AND d.state = 'draft';

ALTER TABLE definition_blobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON definition_blobs
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE definition_versions ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON definition_versions
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

-- goald's grants (00004 sets no default privileges). A blob row is never changed or removed by
-- goald; a never-published factory is deleted with its drafts (DefinitionStore.DeleteUnpublished).
GRANT SELECT, INSERT ON definition_blobs TO goal_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON definition_versions TO goal_app;
GRANT DELETE ON factories TO goal_app;
GRANT SELECT ON factory_records TO goal_app;
