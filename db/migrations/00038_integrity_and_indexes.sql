-- +goose Up
-- Review 2.2 M2, M3, L2 and L4, in one migration since 2.2-E's range has only 00036-00039 left
-- before phase 3's 00040. Indexes are built in this transaction, not CONCURRENTLY: installs are
-- small, and goose runs a migration in one transaction (see README's migration notes).

-- M2: every org-owned table gets a full index that starts with org_id, so goal_purge_org's
-- deletes by org and the cascades from organizations use an index instead of scanning the
-- largest tables. TestSchema_EveryOrgTableHasAnIndexOnOrgID keeps it so for later tables.
CREATE INDEX item_events_org_idx         ON item_events (org_id);
CREATE INDEX item_spans_org_idx          ON item_spans (org_id);
CREATE INDEX lease_spans_org_idx         ON lease_spans (org_id);
CREATE INDEX definition_versions_org_idx ON definition_versions (org_id);
CREATE INDEX andon_pulls_org_idx         ON andon_pulls (org_id);

-- L2: the factory's first lease span (LeaseHistoryStart) without reading and sorting all of
-- them, and the gate verdicts of a window (StationVerdicts) without reading every verdict the
-- factory ever had (item_events_verdicts_idx, newest first by station, stays for the tallies).
CREATE INDEX lease_spans_acquired_idx ON lease_spans (factory_id, acquired_at);
CREATE INDEX item_events_verdicts_at_idx ON item_events (factory_id, at) INCLUDE (station, kind)
  WHERE kind IN ('gate_approved', 'gate_rejected');

-- M3: a work item's parent is an item of its own organization. Foreign keys ignore row-level
-- security, so the plain parent_id key let an item name another organization's item as its
-- parent, and that parent's purge then failed on the key. Added NOT VALID and then validated, the
-- pattern for large tables; existing rows have no parent in another organization, so validation
-- passes. work_items_parent_id_idx (parent_id) serves the key's checks.
ALTER TABLE work_items ADD CONSTRAINT work_items_parent_fkey
  FOREIGN KEY (org_id, parent_id) REFERENCES work_items (org_id, id) NOT VALID;
ALTER TABLE work_items VALIDATE CONSTRAINT work_items_parent_fkey;
ALTER TABLE work_items DROP CONSTRAINT work_items_parent_id_fkey;

-- L4: an item reopened with no span open (it was done or cancelled) carries on in its rework
-- round, so its next span keeps the station that started the round instead of losing rework_by.
-- The rest of 00030's trigger is unchanged.
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

  IF NEW.state IN ('done', 'cancelled') THEN
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
