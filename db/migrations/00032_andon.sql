-- +goose Up
-- Phase 2.2 E9 (spec 2.2-E §6.3): the andon cord. A person pulls it to stop a factory's line (the
-- engine then releases and claims nothing there) and clears it to restart the line; andon_pulls
-- (migration 00001, org-scoped by 00006) keeps every pull as history. The kind is manual in 2.2;
-- rework is reserved for the automatic pull on a gate's rework rate, which is later work.
--
-- Pulls cleared before this migration (no code wrote any) get a clearer so the pair constraint
-- holds.
ALTER TABLE andon_pulls
  ADD COLUMN kind       text NOT NULL DEFAULT 'manual' CHECK (kind IN ('manual', 'rework')),
  ADD COLUMN station    text,
  ADD COLUMN cleared_by text,
  ADD COLUMN note       text,
  ADD COLUMN data       jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data) = 'object');

UPDATE andon_pulls SET cleared_by = 'system:migration' WHERE cleared_at IS NOT NULL;

ALTER TABLE andon_pulls
  ADD CONSTRAINT andon_pulls_cleared CHECK ((cleared_at IS NULL) = (cleared_by IS NULL));

-- A factory's history, newest first (GET …/andon); the open pull uses andon_pulls_open_key.
CREATE INDEX andon_pulls_factory_idx ON andon_pulls (factory_id, pulled_at DESC, id DESC);

-- Realtime: a pull or a clear is the organization's stream event `factory`
-- ({org_id, factory_id, kind: andon_pulled | andon_cleared, andon_id, at}), notified on
-- goal_org_events in the transaction that makes it, like every organization event that is not an
-- item event (postgres.Notices).
-- +goose StatementBegin
CREATE FUNCTION notify_andon() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  kind text := CASE WHEN NEW.cleared_at IS NULL THEN 'andon_pulled' ELSE 'andon_cleared' END;
BEGIN
  PERFORM pg_notify('goal_org_events', json_build_object(
    'org_id', NEW.org_id, 'event', 'factory', 'factory_id', NEW.factory_id,
    'data', json_build_object(
      'org_id', NEW.org_id, 'factory_id', NEW.factory_id, 'kind', kind, 'andon_id', NEW.id,
      'at', coalesce(NEW.cleared_at, NEW.pulled_at)))::text);
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER andon_pulls_notify AFTER INSERT OR UPDATE OF cleared_at ON andon_pulls
  FOR EACH ROW EXECUTE FUNCTION notify_andon();
