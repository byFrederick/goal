-- +goose Up
-- ADR 0031: a run that calls an Ask-first connection tool ends, and its lease ends with the
-- outcome awaiting_approval. The column's CHECK (00064) allowed only the earlier outcomes.
ALTER TABLE lease_spans DROP CONSTRAINT lease_spans_outcome_check;
ALTER TABLE lease_spans ADD CONSTRAINT lease_spans_outcome_check
  CHECK (outcome IN ('advanced', 'rejected', 'escalated', 'blocked', 'released', 'expired', 'taken', 'asked',
    'awaiting_approval'));
