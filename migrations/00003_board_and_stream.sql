-- +goose Up
-- Phase 2: the board, item lists, flow metrics and the realtime stream.

-- Flow metrics read the items a factory finished in a window of time.
CREATE INDEX work_items_done_at_idx ON work_items (factory_id, done_at) WHERE done_at IS NOT NULL;

-- Notifications now carry what a stream client shows without reading the item back: the
-- item's number and its state right after the event (events are appended in the same
-- transaction as the change they record, after it), and when the event happened.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_item_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  item_number bigint;
  item_state  text;
BEGIN
  SELECT number, state INTO item_number, item_state FROM work_items WHERE id = NEW.item_id;
  PERFORM pg_notify('goal_events', json_build_object(
    'id', NEW.id, 'factory_id', NEW.factory_id, 'item_id', NEW.item_id, 'number', item_number,
    'kind', NEW.kind, 'station', NEW.station, 'state', item_state, 'at', NEW.at
  )::text);
  RETURN NEW;
END
$$;
-- +goose StatementEnd
