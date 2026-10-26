-- +goose Up
-- Phase 2.2 E2 (spec 2.2-E §6.1): how long every item spends at each station in each state. A
-- span opens when an item enters a station or changes state or rework round and closes at the
-- next such change; an open item has exactly one open span. A trigger on work_items keeps them,
-- so no code path can forget one. Flow metrics (lead-time bridge, waiting time, the Item screen's
-- time per station) read spans instead of scanning item_events.
CREATE TABLE item_spans (
  id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id           uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id       uuid NOT NULL,
  item_id          uuid NOT NULL,
  type             text NOT NULL,
  class_of_service text NOT NULL,
  station          text NOT NULL,
  state            text NOT NULL CHECK (state IN ('queued', 'ready', 'in_progress', 'waiting_human', 'blocked')),
  rework_round     integer NOT NULL CHECK (rework_round >= 0),
  rework_by        text,                          -- the station that sent the item back; NULL in round 0
  entered_at       timestamptz NOT NULL,
  left_at          timestamptz,                   -- NULL while open
  CHECK (left_at IS NULL OR left_at >= entered_at),
  CONSTRAINT item_spans_factory_fkey FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id),
  CONSTRAINT item_spans_item_fkey FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX item_spans_open_key ON item_spans (item_id) WHERE left_at IS NULL;
CREATE INDEX item_spans_item_idx ON item_spans (item_id, entered_at);
CREATE INDEX item_spans_window_idx ON item_spans (factory_id, left_at);   -- open spans (NULL) included
CREATE INDEX item_spans_station_idx ON item_spans (factory_id, station, entered_at);

ALTER TABLE item_spans ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON item_spans
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
-- The trigger runs as goal_app (SECURITY INVOKER): it inserts spans and closes the open one.
GRANT SELECT, INSERT, UPDATE ON item_spans TO goal_app;

-- The trigger. A change of station, state or rework count closes the open span and, unless the
-- item is now done or cancelled, opens the next one at the change's time (updated_at, which every
-- store path sets to the engine clock's time; a new item enters at created_at). A clock that is
-- behind (another goald) never closes a span before it opened: the change then happens when the
-- open span began. A new rework round is by the station the item left; otherwise the round's
-- station carries on. Under row-level security the item's own organization is the session's, so
-- its spans pass the policy.
-- +goose StatementBegin
CREATE FUNCTION track_item_span() RETURNS trigger LANGUAGE plpgsql AS $$
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
-- Created before the backfill, so it holds a lock on work_items until this migration commits:
-- a change committed by a running goald meanwhile is either in the backfill's snapshot or waits
-- and then fires the trigger.
CREATE TRIGGER work_items_spans AFTER INSERT OR UPDATE OF station, state, rework_count ON work_items
  FOR EACH ROW EXECUTE FUNCTION track_item_span();

-- Backfill: rebuild the spans of every existing item from its events, in event id order. Since
-- phase 1 the engine records every arrival at a human station with a waiting_human event and at
-- the done station with a done event, right after the moved event, so events alone place an item
-- (factories.definition is not read; factories created through B's API have none):
--   created                      → its station, queued (waiting_human when a decision follows there)
--   moved                        → data.to; skipped when the next event (waiting_human or done at
--                                  data.to) places the item; waiting_human when a decision follows
--                                  there, blocked when a retry follows there (an item moved by a
--                                  publish keeps its state), else queued
--   claimed                      → in_progress            blocked → blocked
--   waiting_human                → waiting_human           (an arrival, or a gate escalation)
--   retried, released, lease_expired, agent_cancelled → queued
--   done                         → closes the last span
-- The rework round counts the rejections (gate_rejected, rejected) followed by a move, the round's
-- rework_by is the station of the rejection that started it, and a span ends when the next one
-- begins (times never go backwards). A position equal to the one before opens no span, as with
-- the trigger. The item row is the truth for the open span's state; an item whose last position
-- does not match its station and rework count, or that is cancelled, is left without spans (it
-- still counts in throughput). The integration test TestMigrate_BackfillMatchesTheTrigger proves
-- backfilled spans equal trigger-made ones for the same history.
WITH ev AS (
  SELECT e.item_id, e.id, e.at, e.kind, e.station, e.data->>'to' AS to_station,
         lead(e.kind) OVER w AS next_kind, lead(e.station) OVER w AS next_station
  FROM item_events e
  WHERE e.kind IN ('created', 'moved', 'claimed', 'blocked', 'waiting_human', 'retried', 'released',
                   'lease_expired', 'agent_cancelled', 'done', 'gate_rejected', 'rejected', 'approved', 'merged')
  WINDOW w AS (PARTITION BY e.item_id ORDER BY e.id)
),
rounds AS (
  SELECT ev.*,
         sum((kind IN ('gate_rejected', 'rejected') AND next_kind = 'moved')::int)
           OVER (PARTITION BY item_id ORDER BY id) AS round,
         CASE WHEN kind IN ('gate_rejected', 'rejected') AND next_kind = 'moved' THEN station END AS rejected_at
  FROM ev
),
placed AS (
  SELECT item_id, id, at, round,
         max(rejected_at) OVER (PARTITION BY item_id, round) AS rework_by,
         kind, next_kind, next_station,
         CASE kind WHEN 'moved' THEN to_station ELSE station END AS station
  FROM rounds
),
positions AS (
  SELECT item_id, id, at, round, rework_by, station,
         CASE
           WHEN kind IN ('created', 'moved') THEN
             CASE
               WHEN next_kind IN ('approved', 'rejected', 'merged') AND next_station = station THEN 'waiting_human'
               WHEN kind = 'moved' AND next_kind = 'retried' AND next_station = station THEN 'blocked'
               ELSE 'queued'
             END
           WHEN kind = 'claimed' THEN 'in_progress'
           WHEN kind = 'blocked' THEN 'blocked'
           WHEN kind = 'waiting_human' THEN 'waiting_human'
           WHEN kind = 'done' THEN 'done'
           ELSE 'queued'
         END AS state
  FROM placed
  WHERE kind NOT IN ('gate_rejected', 'rejected', 'approved', 'merged')
    AND NOT coalesce(kind = 'moved' AND next_kind IN ('waiting_human', 'done') AND next_station = station, false)
),
changes AS (
  SELECT p.*, lag(station) OVER w AS prev_station, lag(state) OVER w AS prev_state, lag(round) OVER w AS prev_round
  FROM positions p
  WINDOW w AS (PARTITION BY item_id ORDER BY id)
),
kept AS (
  SELECT item_id, id, station, state, round, rework_by,
         max(at) OVER (PARTITION BY item_id ORDER BY id) AS entered_at
  FROM changes
  WHERE prev_station IS NULL OR (station, state, round) IS DISTINCT FROM (prev_station, prev_state, prev_round)
),
spans AS (
  SELECT k.*, lead(entered_at) OVER w AS left_at, row_number() OVER (PARTITION BY item_id ORDER BY id DESC) AS from_last
  FROM kept k
  WINDOW w AS (PARTITION BY item_id ORDER BY id)
),
placeable AS (
  -- Items whose history ends where they are: open items at their station and rework count, done
  -- items with their done event.
  SELECT s.item_id FROM spans s JOIN work_items w ON w.id = s.item_id
  WHERE s.from_last = 1 AND s.round = w.rework_count
    AND CASE w.state
          WHEN 'done' THEN s.state = 'done'
          WHEN 'cancelled' THEN false
          ELSE s.state <> 'done' AND s.station = w.station
        END
)
INSERT INTO item_spans (org_id, factory_id, item_id, type, class_of_service, station, state,
                        rework_round, rework_by, entered_at, left_at)
SELECT w.org_id, w.factory_id, w.id, w.type, w.class_of_service, s.station,
       CASE WHEN s.from_last = 1 THEN w.state ELSE s.state END,
       s.round, s.rework_by, s.entered_at, s.left_at
FROM spans s
JOIN placeable USING (item_id)
JOIN work_items w ON w.id = s.item_id
WHERE s.state <> 'done'
ORDER BY s.item_id, s.id;
