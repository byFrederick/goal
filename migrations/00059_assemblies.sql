-- +goose Up
-- Assemblies and parts (ADR 0012; Store.Assemble, Parts): an item whose plan needs more than one
-- pull request moves to state 'assembly' and its plan's slices become parts, items whose
-- parent_id is the assembly. part_key names a part within its plan (a retry finds the parts it
-- already created by it) and part_paths are the files the plan says it changes. An assembly is
-- never claimed and opens no item span; the transition that closes its last part closes it.
ALTER TABLE work_items DROP CONSTRAINT work_items_state_check;
ALTER TABLE work_items ADD CONSTRAINT work_items_state_check
  CHECK (state IN ('queued', 'ready', 'in_progress', 'waiting_human', 'blocked', 'done', 'cancelled', 'assembly'))
  NOT VALID;
ALTER TABLE work_items VALIDATE CONSTRAINT work_items_state_check;

ALTER TABLE work_items
  ADD COLUMN part_key   text CHECK (part_key <> ''),
  ADD COLUMN part_paths text[] NOT NULL DEFAULT '{}',
  ADD CONSTRAINT work_items_part_parent_check CHECK (part_key IS NULL OR parent_id IS NOT NULL);
-- One part per key in an assembly; it also serves Parts (by parent, in number order).
CREATE UNIQUE INDEX work_items_parent_part_key_idx ON work_items (parent_id, part_key) WHERE part_key IS NOT NULL;

-- An item in state 'assembly' opens no span, as done and cancelled; the rest of 00038's trigger
-- is unchanged.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION track_item_span() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  prev_by text;
  span_at timestamptz := CASE WHEN TG_OP = 'INSERT' THEN NEW.created_at ELSE NEW.updated_at END;
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF NEW.station = OLD.station AND NEW.state = OLD.state AND NEW.rework_count = OLD.rework_count THEN
      RETURN NEW;
    END IF;

    UPDATE item_spans SET left_at = greatest(NEW.updated_at, entered_at)
     WHERE item_id = NEW.id AND left_at IS NULL
    RETURNING rework_by, left_at INTO prev_by, span_at;
    IF NOT FOUND THEN
      span_at := NEW.updated_at;
      SELECT s.rework_by INTO prev_by FROM item_spans s
       WHERE s.item_id = NEW.id AND s.rework_round = OLD.rework_count
       ORDER BY s.entered_at DESC, s.id DESC LIMIT 1;
    END IF;

    IF NEW.rework_count > OLD.rework_count THEN
      prev_by := OLD.station;
    END IF;
  END IF;

  IF NEW.state IN ('done', 'cancelled', 'assembly') THEN
    RETURN NEW;
  END IF;

  INSERT INTO item_spans (org_id, factory_id, item_id, type, class_of_service, station, state,
                          rework_round, rework_by, entered_at)
  VALUES (NEW.org_id, NEW.factory_id, NEW.id, NEW.type, NEW.class_of_service, NEW.station, NEW.state,
          NEW.rework_count, prev_by, span_at);
  RETURN NEW;
END
$$;
-- +goose StatementEnd
