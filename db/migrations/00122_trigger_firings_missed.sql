-- +goose Up
-- A schedule's missed gap (ADR 0040 decision 16): when goald misses a schedule trigger's instants
-- by more than 5 minutes, it records one firing with outcome 'missed' for the gap, keyed by the
-- gap's first instant and at its last, with how many instants it covers in missed. None of them
-- fires late, and a missed firing has no item. Every other firing's missed is 0.
--
-- Firings are pruned after 90 days, except those whose item is still open, through goal_app's
-- DELETE grant from 00120 (software#654), so this file adds no grant.
ALTER TABLE trigger_firings ADD COLUMN missed integer NOT NULL DEFAULT 0;

ALTER TABLE trigger_firings DROP CONSTRAINT trigger_firings_outcome_check;
ALTER TABLE trigger_firings ADD CONSTRAINT trigger_firings_outcome_check
  CHECK (outcome IN ('created', 'skipped', 'missed')) NOT VALID;
ALTER TABLE trigger_firings VALIDATE CONSTRAINT trigger_firings_outcome_check;

ALTER TABLE trigger_firings ADD CONSTRAINT trigger_firings_missed_check
  CHECK (missed >= 0 AND (outcome = 'missed') = (missed > 0) AND (outcome <> 'missed' OR source = 'schedule')) NOT VALID;
ALTER TABLE trigger_firings VALIDATE CONSTRAINT trigger_firings_missed_check;
