-- +goose Up
-- Knowledge sources read with a secret (ADR 0037 decision 14): a synced source refers to the
-- secret it reads with by id, so renaming the secret keeps the reference. connection_id stays
-- until the migration that drops connections: live sources still read their connection, and a
-- synced source keeps it until moving over gives it a secret. An upload source reads neither; a
-- synced one a connection or a secret; a live one a connection and no secret.
--
-- NO ACTION, checked at the statement's end, as 00054's connection key: an organization's purge
-- removes its secrets and sources in one cascade in whatever order; alone, a secret a source
-- reads with cannot be deleted (ports.OrgSecrets.Delete: ErrConflict).
ALTER TABLE secrets ADD CONSTRAINT secrets_org_id_id_key UNIQUE (org_id, id);

ALTER TABLE knowledge_sources ADD COLUMN secret_id uuid;
ALTER TABLE knowledge_sources
  ADD CONSTRAINT knowledge_sources_secret_fkey FOREIGN KEY (org_id, secret_id) REFERENCES secrets (org_id, id);

-- 00054's CHECK ((kind = 'upload') = (connection_id IS NULL)), unnamed there.
ALTER TABLE knowledge_sources DROP CONSTRAINT knowledge_sources_check;
ALTER TABLE knowledge_sources ADD CONSTRAINT knowledge_sources_reads_check CHECK (CASE kind
  WHEN 'upload' THEN connection_id IS NULL AND secret_id IS NULL
  WHEN 'synced' THEN connection_id IS NOT NULL OR secret_id IS NOT NULL
  ELSE connection_id IS NOT NULL AND secret_id IS NULL END);

-- A secret's deletion checks the sources that read with it.
CREATE INDEX knowledge_sources_secret_idx ON knowledge_sources (org_id, secret_id) WHERE secret_id IS NOT NULL;

GRANT UPDATE (secret_id) ON knowledge_sources TO goal_app;
