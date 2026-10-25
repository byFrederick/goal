-- +goose Up
-- Phase 4 review (review 4 SQL L2, L3, L4, L5): the smaller fixes to 00052 and 00054.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- L2. A touch label is its event's, of the event's item and organization. event_id's key ignores
-- row-level security, so a label could take another organization's event (one of before 00052,
-- which has none), and that organization's own label of the event then failed on a row it cannot
-- see. The check runs with the inserting role's rights, under which another organization's event
-- does not exist.
-- +goose StatementBegin
CREATE FUNCTION goal_touch_label_of_event() RETURNS trigger
LANGUAGE plpgsql SET search_path FROM CURRENT
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM item_events e
                 WHERE e.id = NEW.event_id AND e.org_id = NEW.org_id AND e.item_id = NEW.item_id) THEN
    RAISE EXCEPTION 'touch label of event %: no such event of the item in the organization', NEW.event_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'item_touch_labels_event_of_item';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION goal_touch_label_of_event() FROM PUBLIC;
CREATE TRIGGER item_touch_labels_event_of_item BEFORE INSERT ON item_touch_labels
  FOR EACH ROW EXECUTE FUNCTION goal_touch_label_of_event();

-- L5. 00052's trigger function resolved item_touch_labels through the caller's search_path; it
-- is pinned like every other phase 4 function. (goal_touch_planned reads no relation, and a SET
-- clause would stop the planner inlining it into item_touches.)
ALTER FUNCTION goal_label_touch_event() SET search_path FROM CURRENT;

-- L3. UNIQUE (org_id, source_id, id) is that same index already.
DROP INDEX knowledge_documents_org_source_idx;

-- L4. An external id is bounded in bytes (domain.MaxExternalID), so its UNIQUE (source_id,
-- external_id) entry fits a btree page: 1,024 characters of a script of three or four bytes each
-- did not, and the document failed to store. The ingestor leaves such a document out. Rows
-- already stored are not checked again (NOT VALID): they fit when they were written.
ALTER TABLE knowledge_documents DROP CONSTRAINT knowledge_documents_external_id_check;
ALTER TABLE knowledge_documents ADD CONSTRAINT knowledge_documents_external_id_check
  CHECK (octet_length(external_id) BETWEEN 1 AND 2000) NOT VALID;
