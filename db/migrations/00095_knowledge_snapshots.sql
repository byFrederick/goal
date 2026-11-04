-- +goose Up
-- Knowledge is files (ADR 0038 decisions 1, 3 and 15), the additive half: a source becomes a
-- folder of Markdown files kept in sync, written as zip snapshots. This migration only adds.
-- knowledge_documents, knowledge_chunks, knowledge_uses, knowledge_verdicts, connection_id and
-- the vector extension stay until the cut-over (decision 16) converts the sources and drops them.
--
-- knowledge_sources gains read_when, the line that tells agents when to open the source, and its
-- current snapshot. Its config (00054) is already the kind's settings and its secret_id (00089)
-- the ADR 0037 secret it reads with, by id so a rename keeps it. The kinds become upload,
-- repository and confluence; synced and live stay allowed until the cut-over converts their rows.
--
-- knowledge_snapshots is one row per zip a sync wrote, at its zip_key, with the read-when line
-- the snapshot was made with, so a run's prompt comes from what it pinned. knowledge_files is the
-- current snapshot's files: a new snapshot replaces them. knowledge_reads is the daily rollup of
-- reads and searches per source, path and factory. factory_id has no foreign key: a rollup row
-- outlives its factory, and an organization's purge removes it.
--
-- Paths are COLLATE "C": they sort and page byte by byte, as goald compares them.

CREATE TABLE knowledge_snapshots (
  id         uuid PRIMARY KEY,                                             -- UUIDv7 from goald
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  source_id  uuid NOT NULL,
  files      int NOT NULL CHECK (files BETWEEN 0 AND 20000),               -- domain.MaxSourceFiles
  failed     int NOT NULL DEFAULT 0 CHECK (failed >= 0),                   -- files kept at their last good version or left out
  bytes      bigint NOT NULL CHECK (bytes BETWEEN 0 AND 52428800),         -- its files' Markdown, domain.MaxSourceBytes
  zip_bytes  bigint NOT NULL CHECK (zip_bytes >= 0),                       -- the zip's size in the bucket
  changes    jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(changes) = 'object' AND octet_length(changes::text) <= 65536),
  read_when  text NOT NULL DEFAULT '' CHECK (length(read_when) <= 160),
  zip_key    text NOT NULL GENERATED ALWAYS AS
               ('orgs/' || org_id::text || '/knowledge/' || source_id::text || '/' || id::text || '.zip') STORED,
  created_at timestamptz NOT NULL,
  FOREIGN KEY (org_id, source_id) REFERENCES knowledge_sources (org_id, id) ON DELETE CASCADE,
  UNIQUE (org_id, id)
);
-- A source's snapshots, newest first.
CREATE INDEX knowledge_snapshots_source_idx ON knowledge_snapshots (org_id, source_id, created_at DESC, id DESC);

CREATE TABLE knowledge_files (
  org_id    uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  source_id uuid NOT NULL,
  path      text COLLATE "C" NOT NULL CHECK (octet_length(path) BETWEEN 1 AND 1024),
  title     text NOT NULL CHECK (length(title) <= 1024),
  bytes     bigint NOT NULL CHECK (bytes BETWEEN 0 AND 5242880),           -- domain.MaxFileBytes
  version   text NOT NULL CHECK (length(version) BETWEEN 1 AND 1024),     -- content hash, blob id, page version
  PRIMARY KEY (org_id, source_id, path),
  FOREIGN KEY (org_id, source_id) REFERENCES knowledge_sources (org_id, id) ON DELETE CASCADE
);

CREATE TABLE knowledge_reads (
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  source_id  uuid NOT NULL,
  path       text COLLATE "C" NOT NULL CHECK (octet_length(path) BETWEEN 1 AND 1024),
  factory_id uuid NOT NULL,
  day        date NOT NULL,
  reads      int NOT NULL DEFAULT 0 CHECK (reads >= 0),
  searches   int NOT NULL DEFAULT 0 CHECK (searches >= 0),
  PRIMARY KEY (org_id, source_id, path, factory_id, day),
  FOREIGN KEY (org_id, source_id) REFERENCES knowledge_sources (org_id, id) ON DELETE CASCADE
);

ALTER TABLE knowledge_sources
  ADD COLUMN read_when text NOT NULL DEFAULT '' CHECK (length(read_when) <= 160 AND read_when !~ '[\r\n]'),
  ADD COLUMN current_snapshot_id uuid;
-- NO ACTION: the current snapshot cannot be deleted; a source's deletion takes its snapshots with it.
ALTER TABLE knowledge_sources
  ADD CONSTRAINT knowledge_sources_current_snapshot_fkey
  FOREIGN KEY (org_id, current_snapshot_id) REFERENCES knowledge_snapshots (org_id, id);

-- 00054's kind check, unnamed there.
ALTER TABLE knowledge_sources DROP CONSTRAINT knowledge_sources_kind_check;
ALTER TABLE knowledge_sources ADD CONSTRAINT knowledge_sources_kind_check
  CHECK (kind IN ('upload', 'repository', 'confluence', 'synced', 'live'));

-- 00089's: a repository reads with a secret or none (a public one), Confluence with a secret;
-- neither reads a connection.
ALTER TABLE knowledge_sources DROP CONSTRAINT knowledge_sources_reads_check;
ALTER TABLE knowledge_sources ADD CONSTRAINT knowledge_sources_reads_check CHECK (CASE kind
  WHEN 'upload' THEN connection_id IS NULL AND secret_id IS NULL
  WHEN 'repository' THEN connection_id IS NULL
  WHEN 'confluence' THEN connection_id IS NULL AND secret_id IS NOT NULL
  WHEN 'synced' THEN connection_id IS NOT NULL OR secret_id IS NOT NULL
  ELSE connection_id IS NOT NULL AND secret_id IS NULL END);

ALTER TABLE knowledge_snapshots ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON knowledge_snapshots
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE knowledge_files ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON knowledge_files
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE knowledge_reads ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON knowledge_reads
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON knowledge_snapshots, knowledge_files, knowledge_reads TO goal_app;
GRANT UPDATE (reads, searches) ON knowledge_reads TO goal_app;
GRANT UPDATE (read_when, current_snapshot_id) ON knowledge_sources TO goal_app;
