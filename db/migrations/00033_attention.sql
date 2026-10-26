-- +goose Up
-- Phase 2.2 E11 (spec 2.2-E §6.4): the organization's inbox reads the few open items that may
-- need a person across its factories (FlowStats.InboxItems): waiting for a decision, blocked, or
-- in the expedite lane, the most recently changed first. The partial index keeps that read on
-- those rows only, whatever the size of the organization's history; its predicate is the
-- query's. Row-level security still binds the read to the organization.
CREATE INDEX work_items_attention_idx ON work_items (org_id, state, updated_at)
  WHERE state IN ('waiting_human', 'blocked')
     OR (class_of_service = 'expedite' AND state NOT IN ('done', 'cancelled'));
