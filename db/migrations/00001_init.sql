-- +goose Up
-- Goal's whole state. PostgreSQL 18 (uuidv7). Forward-only: never edit an applied migration.
--
-- Station, work type and agent names are TEXT, not enums: they come from each factory's
-- factory.yaml, so a new factory never needs a schema change.

-- One row per factory, keyed by the immutable metadata.id from factory.yaml.
CREATE TABLE factories (
  id                uuid PRIMARY KEY,
  name              text NOT NULL CHECK (length(name) BETWEEN 3 AND 40),
  title             text,
  definition        jsonb NOT NULL,                -- the parsed factory.yaml currently loaded
  definition_sha256 text NOT NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);
-- Names are unique but renameable; nothing else references them.
CREATE UNIQUE INDEX factories_name_key ON factories (lower(name));

CREATE TABLE work_items (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id       uuid NOT NULL REFERENCES factories (id),
  number           bigint NOT NULL,               -- shown as <FACTORY>-<number>
  parent_id        uuid REFERENCES work_items (id),
  type             text NOT NULL,
  lane             text,
  title            text NOT NULL CHECK (length(title) <= 300),
  body             text NOT NULL DEFAULT '',
  station          text NOT NULL,
  state            text NOT NULL DEFAULT 'queued'
                   CHECK (state IN ('queued', 'ready', 'in_progress', 'waiting_human', 'blocked', 'done', 'cancelled')),
  class_of_service text NOT NULL DEFAULT 'standard' CHECK (class_of_service IN ('standard', 'expedite')),
  priority         smallint NOT NULL DEFAULT 0,
  size             text CHECK (size IN ('S', 'M', 'L')),
  rework_count     integer NOT NULL DEFAULT 0 CHECK (rework_count >= 0),
  cost_usd         numeric(12, 4) NOT NULL DEFAULT 0,
  outputs          jsonb NOT NULL DEFAULT '{}',   -- station id → contract output
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  done_at          timestamptz,
  UNIQUE (factory_id, number)
);
CREATE INDEX work_items_parent_id_idx ON work_items (parent_id) WHERE parent_id IS NOT NULL;
-- The pull query: next item at a station, expedite first, then priority, then age.
CREATE INDEX work_items_pull_idx ON work_items
  (factory_id, station, (class_of_service = 'expedite') DESC, priority DESC, created_at)
  WHERE state IN ('queued', 'ready');
CREATE INDEX work_items_open_idx ON work_items (factory_id, station) WHERE state NOT IN ('done', 'cancelled');

-- One agent container working one item. Rows disappear on release; expired leases go back to the queue.
CREATE TABLE leases (
  item_id      uuid PRIMARY KEY REFERENCES work_items (id) ON DELETE CASCADE,
  factory_id   uuid NOT NULL REFERENCES factories (id),
  station      text NOT NULL,
  agent        text NOT NULL,
  slot         integer NOT NULL CHECK (slot >= 0),
  container    text,
  acquired_at  timestamptz NOT NULL DEFAULT now(),
  heartbeat_at timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  UNIQUE (factory_id, station, slot)
);
CREATE INDEX leases_expires_at_idx ON leases (expires_at);

CREATE TABLE agent_runs (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id    uuid NOT NULL REFERENCES factories (id),
  item_id       uuid NOT NULL REFERENCES work_items (id),
  station       text NOT NULL,
  agent         text NOT NULL,
  model         text NOT NULL,
  session_id    text,
  status        text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
  turns         integer NOT NULL DEFAULT 0,
  input_tokens  bigint NOT NULL DEFAULT 0,
  output_tokens bigint NOT NULL DEFAULT 0,
  cost_usd      numeric(12, 4) NOT NULL DEFAULT 0,
  error         text,
  started_at    timestamptz NOT NULL DEFAULT now(),
  ended_at      timestamptz
);
CREATE INDEX agent_runs_item_id_idx ON agent_runs (item_id);
CREATE INDEX agent_runs_factory_started_idx ON agent_runs (factory_id, started_at DESC);

-- The source of truth for flow metrics and traces. Append-only.
CREATE TABLE item_events (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  factory_id uuid NOT NULL REFERENCES factories (id),
  item_id    uuid NOT NULL REFERENCES work_items (id),
  run_id     uuid REFERENCES agent_runs (id),
  at         timestamptz NOT NULL DEFAULT now(),
  kind       text NOT NULL,   -- created, moved, claimed, released, tool_call, output, check_failed, gate_passed, gate_rejected, ...
  station    text,
  actor      text NOT NULL,   -- an agent name, 'engine' or 'human'
  data       jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX item_events_item_idx ON item_events (item_id, id);
CREATE INDEX item_events_factory_at_idx ON item_events (factory_id, at);
CREATE INDEX item_events_run_id_idx ON item_events (run_id) WHERE run_id IS NOT NULL;

CREATE TABLE andon_pulls (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  factory_id uuid NOT NULL REFERENCES factories (id),
  reason     text NOT NULL,
  pulled_by  text NOT NULL,   -- 'engine' (rework threshold) or 'human'
  pulled_at  timestamptz NOT NULL DEFAULT now(),
  cleared_at timestamptz
);
-- At most one open pull per factory.
CREATE UNIQUE INDEX andon_pulls_open_key ON andon_pulls (factory_id) WHERE cleared_at IS NULL;

-- Realtime: goald LISTENs on goal_events and fans out to the UI over Server-Sent Events.
-- +goose StatementBegin
CREATE FUNCTION notify_item_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('goal_events', json_build_object(
    'id', NEW.id, 'factory_id', NEW.factory_id, 'item_id', NEW.item_id, 'kind', NEW.kind, 'station', NEW.station
  )::text);
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER item_events_notify AFTER INSERT ON item_events FOR EACH ROW EXECUTE FUNCTION notify_item_event();

-- WIP per station, read by the board and the constraint detector.
CREATE VIEW station_wip AS
SELECT factory_id, station,
       count(*) FILTER (WHERE state = 'in_progress')                     AS in_progress,
       count(*) FILTER (WHERE state IN ('queued', 'ready'))              AS waiting,
       min(created_at) FILTER (WHERE state IN ('queued', 'ready'))       AS oldest_waiting_since
FROM work_items
WHERE state NOT IN ('done', 'cancelled')
GROUP BY factory_id, station;
