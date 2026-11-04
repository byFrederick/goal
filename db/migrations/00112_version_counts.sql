-- +goose Up
-- A published version's counts (stations, agents, skills, connections), kept on its row so the
-- version list reads no files from the bucket. Publish writes them; a version published before
-- this has none (NULL) until the first list computes them from its tree and stores them.
ALTER TABLE definition_versions
  ADD COLUMN counts jsonb CHECK (counts IS NULL OR jsonb_typeof(counts) = 'object');

-- Published and archived versions never change, except that a published version without counts
-- may be given them once; nothing else of the row may change with them.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION definition_versions_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.state = 'published' AND OLD.counts IS NULL AND NEW.counts IS NOT NULL
     AND to_jsonb(NEW) - 'counts' = to_jsonb(OLD) - 'counts' THEN
    RETURN NEW;
  END IF;
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

-- A version row written: a draft opened, saved, discarded or published ('draft'). Storing a
-- published version's counts is the only update of a row that is no longer a draft, and no one
-- needs to hear of it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_definition_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.state <> 'draft' THEN
    RETURN NEW;
  END IF;
  PERFORM pg_notify('goal_definitions', json_build_object(
    'org_id', NEW.org_id, 'factory_id', NEW.factory_id, 'kind', 'draft', 'version_id', NEW.id,
    'number', NEW.number, 'revision', NEW.revision, 'by', NEW.updated_by)::text);
  RETURN NEW;
END
$$;
-- +goose StatementEnd
