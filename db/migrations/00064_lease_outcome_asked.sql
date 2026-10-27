-- +goose Up
-- ADR 0018: an agent that asks a person a question ends its run, and its lease ends with the
-- outcome asked. The column's CHECK (00031) allowed only the earlier outcomes.
ALTER TABLE lease_spans DROP CONSTRAINT lease_spans_outcome_check;
ALTER TABLE lease_spans ADD CONSTRAINT lease_spans_outcome_check
  CHECK (outcome IN ('advanced', 'rejected', 'escalated', 'blocked', 'released', 'expired', 'taken', 'asked'));
