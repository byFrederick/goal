-- +goose Up
-- A Jonah review's run (kind jonah, ADR 0034 decision 1) works no item: engine.RunTask records it
-- without one. item_id becomes nullable, and agent_runs_item_check keeps it required for every
-- other run, so a line's run still names the item it worked. The check is added NOT VALID and
-- then validated, as 00043 does; every existing row has an item.
--
-- Queries that join agent_runs to work_items on item_id (the run list, lost runs, flow and
-- touches) leave a run without an item out, which is what they mean: none of them is about a
-- review. FinishRun adds no item cost and writes no item event for such a run (item_events.item_id
-- stays NOT NULL).
ALTER TABLE agent_runs ALTER COLUMN item_id DROP NOT NULL;
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_item_check CHECK (item_id IS NOT NULL OR kind = 'jonah') NOT VALID;
ALTER TABLE agent_runs VALIDATE CONSTRAINT agent_runs_item_check;
