-- +goose Up
-- The conversion in goald (ADR 0040 decision 15) moves each factory's webhook endpoint to the
-- organization: it clears factory_id and sets sender and secret_id beside the new name. goal_app
-- may update only name and enabled (00115) and the rotate columns (00071), so it gains UPDATE on
-- those three. The sealed key_id, nonce and ciphertext stay until the cleanup (decision 24).
GRANT UPDATE (factory_id, sender, secret_id) ON webhook_endpoints TO goal_app;

