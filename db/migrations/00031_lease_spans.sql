-- +goose Up
-- Phase 2.2 E3 (spec 2.2-E §6.2): how long every WIP slot was held and how that ended. The store
-- writes one row in the transaction that deletes a lease: a transition that releases it (with the
-- outcome the engine gives) or the sweep that requeues an expired one (expired). Busy shares,
-- task times and agent statistics read these rows; there is no backfill, so those statistics
-- report how far back their history goes until the window is covered.
CREATE TABLE lease_spans (
  id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id       uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id   uuid NOT NULL,
  item_id      uuid NOT NULL,
  station      text NOT NULL,
  agent        text NOT NULL,
  slot         integer NOT NULL CHECK (slot >= 0),
  rework_round integer NOT NULL CHECK (rework_round >= 0),   -- the item's rework count while held
  acquired_at  timestamptz NOT NULL,
  ended_at     timestamptz NOT NULL CHECK (ended_at >= acquired_at),
  outcome      text NOT NULL
               CHECK (outcome IN ('advanced', 'rejected', 'escalated', 'blocked', 'released', 'expired', 'taken')),
  CONSTRAINT lease_spans_factory_fkey FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id),
  CONSTRAINT lease_spans_item_fkey FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE
);
CREATE INDEX lease_spans_station_idx ON lease_spans (factory_id, station, ended_at);
CREATE INDEX lease_spans_agent_idx ON lease_spans (factory_id, agent, ended_at);
CREATE INDEX lease_spans_item_idx ON lease_spans (item_id);

ALTER TABLE lease_spans ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON lease_spans
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
-- Append-only: goal_app records a lease once and never changes it.
GRANT SELECT, INSERT ON lease_spans TO goal_app;

-- Gate verdicts at a station, newest first: the andon's rework tally, a station's "sends back"
-- share and the agent page's reasons.
CREATE INDEX item_events_verdicts_idx ON item_events (factory_id, station, id DESC)
  WHERE kind IN ('gate_approved', 'gate_rejected');
-- An item's rejections in time: which send-back started a rework round.
CREATE INDEX item_events_rejections_idx ON item_events (item_id, at)
  WHERE kind IN ('gate_rejected', 'rejected');
