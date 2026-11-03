-- +goose Up
-- A source may sync only when someone presses Sync (ADR 0038 decision 1): a NULL sync_every says
-- so, and its next_sync_at is NULL between syncs, as a live source's always is. Sync now makes it
-- due once, and the sync's end sets next_sync_at back to NULL (FinishKnowledgeSync: at plus a
-- NULL interval). goal_claim_knowledge_source and the due indexes stay: a NULL next_sync_at never
-- matches next_sync_at <= p_now.

-- 00054's sync_every interval NOT NULL CHECK (sync_every >= interval '15 minutes').
ALTER TABLE knowledge_sources ALTER COLUMN sync_every DROP NOT NULL;
ALTER TABLE knowledge_sources DROP CONSTRAINT knowledge_sources_sync_every_check;
ALTER TABLE knowledge_sources ADD CONSTRAINT knowledge_sources_sync_every_check
  CHECK (sync_every IS NULL OR sync_every >= interval '15 minutes');

-- 00054's CHECK ((kind = 'live') = (next_sync_at IS NULL)), unnamed there: a live source still
-- never syncs, and a source of any other kind may wait for Sync.
ALTER TABLE knowledge_sources DROP CONSTRAINT knowledge_sources_check1;
ALTER TABLE knowledge_sources ADD CONSTRAINT knowledge_sources_live_check CHECK (kind <> 'live' OR next_sync_at IS NULL);
