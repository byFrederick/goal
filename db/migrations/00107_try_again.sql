-- +goose Up
-- Try again (ADR 0039 decision 11): a person adds tries to an open proposal, one more per item it
-- was tried on, or three per item for a model proposal. Each new try of an item takes the attempt
-- after the item's highest, so tries.attempt has no upper bound any more; tries_key still keeps
-- each (proposal, item, attempt) once. Every row stored before has an attempt of 1 to 3, so it
-- stays valid.
ALTER TABLE tries DROP CONSTRAINT tries_attempt_check;
ALTER TABLE tries ADD CONSTRAINT tries_attempt_check CHECK (attempt >= 1) NOT VALID;
ALTER TABLE tries VALIDATE CONSTRAINT tries_attempt_check;
