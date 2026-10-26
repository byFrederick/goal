-- +goose Up
-- Phase 4 (4-23): human touches per item and their reasons (spec 4 §4.9, §4.10). A touch is one
-- action by a person on an item: the item's events with the same person as data.actor (user:…,
-- user:local included) and the same at, other than created and lease_taken, named by the most
-- specific of them. Touches are derived from the events already recorded, so existing items
-- count from the first release; nothing new is stored to count them. domain.TouchesOf keeps the
-- same rule for the in-memory store, and storetest checks both agree. Indexes are built in this
-- transaction, as 00038 explains.

-- Every touch read starts from an item (its touches, the touches of the items finished in a
-- window), so person events are indexed by item. The predicate is the view's, word for word, so
-- the planner can use the index.
CREATE INDEX item_events_person_idx ON item_events (item_id, at) WHERE data->>'actor' LIKE 'user:%';

-- A touch event's reason (missing_knowledge, wrong_decision, required_gate, other): a row per
-- person event, added by the trigger below; a touch that groups several events reads the row of
-- the event that names it. Older events have none until a person sets one (Store.LabelTouch,
-- audited as touch.labelled, which upserts). reason_by is who chose it; NULL for the trigger's
-- planned default. Phase 5 adds columns (agent-suggested reasons, repeat-guidance flags).
CREATE TABLE item_touch_labels (
  event_id   bigint PRIMARY KEY REFERENCES item_events (id) ON DELETE CASCADE,
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  item_id    uuid NOT NULL,
  reason     text CHECK (reason IN ('missing_knowledge', 'wrong_decision', 'required_gate', 'other')),
  reason_by  text CHECK (reason_by ~ '^(user|service|agent|system):.+$'),  -- Actor.Ref()
  updated_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE
);
CREATE INDEX item_touch_labels_org_item_idx ON item_touch_labels (org_id, item_id);

ALTER TABLE item_touch_labels ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON item_touch_labels
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
-- goald inserts rows (the trigger runs as goal_app) and sets a reason; it never deletes one: rows
-- go with their event, item or organization.
GRANT SELECT, INSERT ON item_touch_labels TO goal_app;
GRANT UPDATE (reason, reason_by, updated_at) ON item_touch_labels TO goal_app;

-- goal_touch_planned is whether a person's event is a decision the line requires: data.planned
-- when the event carries a boolean one (decisions do from 4-23 on), else whether it is an
-- approval, gate approval or merge, as older decisions count (domain.PlannedEvent).
-- +goose StatementBegin
CREATE FUNCTION goal_touch_planned(p_kind text, p_data jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
  SELECT CASE jsonb_typeof(p_data->'planned')
           WHEN 'boolean' THEN (p_data->'planned')::boolean
           ELSE p_kind IN ('approved', 'gate_approved', 'merged')
         END
$$;
-- +goose StatementEnd

-- A row for every new touch event, reason required_gate when planned. It runs with the inserting
-- role's rights, under the event's own organization.
-- +goose StatementBegin
CREATE FUNCTION goal_label_touch_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO item_touch_labels (event_id, org_id, item_id, reason)
  VALUES (NEW.id, NEW.org_id, NEW.item_id,
          CASE WHEN goal_touch_planned(NEW.kind, NEW.data) THEN 'required_gate' END);
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER item_events_touch_label AFTER INSERT ON item_events FOR EACH ROW
  WHEN (NEW.data->>'actor' LIKE 'user:%' AND NEW.kind NOT IN ('created', 'lease_taken'))
  EXECUTE FUNCTION goal_label_touch_event();

-- item_touches is every touch, one row each, named by its event of lowest rank (merged, rejected,
-- gate_rejected, gate_approved, approved, moved, paused, retried, note, released, then any other
-- kind; the earliest event on a tie), with the reason of that event's row. Read it by item_id
-- (one item, or LATERAL per item), which the planner pushes into the DISTINCT ON.
CREATE VIEW item_touches WITH (security_invoker = true) AS
SELECT DISTINCT ON (e.item_id, e.data->>'actor', e.at)
       e.org_id, e.factory_id, e.item_id, e.id AS event_id, e.kind, e.station, e.at,
       (e.data->>'actor')::text AS actor, e.actor AS actor_name,
       goal_touch_planned(e.kind, e.data) AS planned, l.reason, l.reason_by
FROM item_events e
LEFT JOIN item_touch_labels l ON l.event_id = e.id
WHERE e.data->>'actor' LIKE 'user:%' AND e.kind NOT IN ('created', 'lease_taken')
ORDER BY e.item_id, e.data->>'actor', e.at,
         array_position(ARRAY['merged', 'rejected', 'gate_rejected', 'gate_approved', 'approved', 'moved', 'paused',
                              'retried', 'note', 'released'], e.kind) NULLS LAST,
         e.id;
GRANT SELECT ON item_touches TO goal_app;

