-- +goose Up
-- An archived factory (DefinitionStore.ArchiveFactory): retired by its organization, paused and
-- idle, still readable with its items, runs and versions, and restorable. archived_at is set
-- only on a paused factory none of whose items holds a lease; the adapter checks both under the
-- factory's row lock, since a CHECK cannot see leases. A different name from a discarded draft's
-- state 'archived' in definition_versions, which is a version's, not a factory's.
ALTER TABLE factories
  ADD COLUMN archived_at timestamptz,
  ADD COLUMN archived_by text CHECK (archived_by ~ '^(user|service|agent|system):.+$'),  -- Actor.Ref()
  ADD CONSTRAINT factories_archived_stamp CHECK ((archived_at IS NULL) = (archived_by IS NULL));

-- Archiving and restoring move the factory as pausing does: every goald refreshes its lines
-- ('current', as 00010's trigger sends for a pause).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_factory_definition() RETURNS trigger LANGUAGE plpgsql AS $$
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
     AND NEW.paused_at IS NOT DISTINCT FROM OLD.paused_at
     AND NEW.archived_at IS NOT DISTINCT FROM OLD.archived_at THEN
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
DROP TRIGGER factories_notify_definition ON factories;
CREATE TRIGGER factories_notify_definition AFTER INSERT OR DELETE OR UPDATE OF current_version_id, paused_at, archived_at
  ON factories FOR EACH ROW EXECUTE FUNCTION notify_factory_definition();

-- ports.FactoryRecord's archive stamp. The new columns go last, so CREATE OR REPLACE keeps the
-- view, its goal_app grant and the columns sqlc scans; it still runs with the caller's rights.
CREATE OR REPLACE VIEW factory_records WITH (security_invoker = true) AS
SELECT f.id, f.org_id, f.name, f.title, f.last_version_number, f.paused_at, f.paused_by, f.created_at, f.created_by,
       c.id AS current_id, c.number AS current_number, c.revision AS current_revision,
       c.published_at AS current_at, c.published_by AS current_by,
       d.id AS draft_id, d.revision AS draft_revision, d.updated_at AS draft_at, d.updated_by AS draft_by,
       f.archived_at, f.archived_by
FROM factories f
LEFT JOIN definition_versions c ON c.id = f.current_version_id
LEFT JOIN definition_versions d ON d.factory_id = f.id AND d.state = 'draft';
