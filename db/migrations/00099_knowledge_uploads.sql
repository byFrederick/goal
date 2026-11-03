-- +goose Up
-- Uploads without documents (ADR 0038 decision 2): an upload source's files are rows of
-- knowledge_uploads, a name and the content hash of the original, which stays in the bucket. The
-- old document and chunk tables, with the uses and verdicts that pointed at them, go: files are
-- read through snapshots and counted in knowledge_reads. The vector extension stays: the
-- administrator owns it, so no migration can drop it.
CREATE TABLE knowledge_uploads (
  id          uuid PRIMARY KEY,                                            -- UUIDv7 from goald
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  source_id   uuid NOT NULL,
  name        text COLLATE "C" NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 1024),
  mime        text NOT NULL CHECK (length(mime) <= 255),
  bytes       bigint NOT NULL CHECK (bytes >= 0),
  blob_sha256 text NOT NULL CHECK (blob_sha256 ~ '^[0-9a-f]{64}$'),        -- orgs/<org-id>/knowledge/<sha256>
  created_by  text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),
  created_at  timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (org_id, source_id) REFERENCES knowledge_sources (org_id, id) ON DELETE CASCADE,
  UNIQUE (source_id, name)
);
-- Whether another upload still holds an original's content, before its object goes.
CREATE INDEX knowledge_uploads_blob_idx ON knowledge_uploads (org_id, blob_sha256);

ALTER TABLE knowledge_uploads ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON knowledge_uploads
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
GRANT SELECT, INSERT, DELETE ON knowledge_uploads TO goal_app;
GRANT UPDATE (mime, bytes, blob_sha256) ON knowledge_uploads TO goal_app;

-- Every upload document of an upload source becomes a file. Two with one name keep the newer.
INSERT INTO knowledge_uploads (id, org_id, source_id, name, mime, bytes, blob_sha256, created_by, created_at)
SELECT DISTINCT ON (d.source_id, d.title) d.id, d.org_id, d.source_id, d.title, d.mime, d.bytes, d.blob_sha256, d.created_by, d.created_at
FROM knowledge_documents d
JOIN knowledge_sources s ON s.org_id = d.org_id AND s.id = d.source_id AND s.kind = 'upload'
WHERE d.blob_sha256 IS NOT NULL AND length(d.title) >= 1
ORDER BY d.source_id, d.title, d.created_at DESC, d.id DESC;

DROP TABLE knowledge_verdicts;
DROP TABLE knowledge_uses;
DROP TABLE knowledge_chunks;
DROP TABLE knowledge_documents;

-- The next sync of each upload source reads its files again.
UPDATE knowledge_sources SET next_sync_at = now() WHERE kind = 'upload';
