-- +goose Up
-- When the receiver last accepted GitHub's ping to a webhook (ADR 0040 decision 12), shown on the
-- webhook as "Received GitHub's ping". A ping no enabled trigger names keeps no delivery row, only
-- its ledger id and daily count, so its time is kept on the webhook, set in the receipt's
-- transaction; a duplicate ping leaves it. goal_app may update that column only.
ALTER TABLE webhook_endpoints ADD COLUMN pinged_at timestamptz;

GRANT UPDATE (pinged_at) ON webhook_endpoints TO goal_app;
