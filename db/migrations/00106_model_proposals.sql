-- +goose Up
-- Try another model (ADR 0039 decision 13): a person picks one model the organization's policy
-- allows for an agent, and goald writes an ordinary proposal that changes the agent's
-- agents[].model, with three tries per item on the agent's newest finished items.
--
-- A model proposal has no review and undoes nothing: agent names the agent whose model it changes,
-- and the review check 00111 writes accepts it as a third kind. Every row stored before has a review
-- or undoes, so it stays valid.
--
-- tries.attempt numbers a proposal's tries of one item, 1 to 3: a review's proposal has one per
-- item (attempt 1, the default every row before has), a model proposal three. tries_key becomes
-- (org_id, proposal_id, item_id, attempt), so each is queued once. goal_app inserts the column and
-- never updates it.
ALTER TABLE jonah_proposals ADD COLUMN agent text CHECK (length(agent) BETWEEN 1 AND 100);

ALTER TABLE tries ADD COLUMN attempt integer NOT NULL DEFAULT 1 CHECK (attempt BETWEEN 1 AND 3);
ALTER TABLE tries DROP CONSTRAINT tries_key;
ALTER TABLE tries ADD CONSTRAINT tries_key UNIQUE (org_id, proposal_id, item_id, attempt);
