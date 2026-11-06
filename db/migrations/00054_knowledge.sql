-- +goose Up
-- Phase 4 (4-16): the knowledge base (spec 4 §4.8, §6). One per organization, made of sources:
-- uploads (originals in the bucket at orgs/<org-id>/knowledge/<sha256>), synced connections and
-- live connections (never indexed). A source's documents are split into chunks, each with a
-- keyword index (a simple tsvector, any language). A goald syncs a source under a lease
-- (lease_until), so any instance may sync any source and the lease of one that died ends. Every
-- row is the organization's, under row-level security.
--
-- Edited after it was applied (2026-11-06, ADR 0038's amendment), against the database rule
-- "Never edit an applied migration" on purpose: this file used to stop without pgvector's vector
-- extension, so a fresh install on plain PostgreSQL failed here, before any later file could
-- fix it. Goose never re-runs an applied file, so no database that ran the old one sees this one.
-- Where the extension exists, the guarded block below adds what the old file had: each chunk's
-- embedding (halfvec of 1024 dimensions, HNSW by cosine distance) and its CHECK with embed_model.
-- Without it, chunks get no embedding. 00098 drops the embeddings either way.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE TABLE knowledge_sources (
  id             uuid PRIMARY KEY,                                         -- UUIDv7 from goald
  org_id         uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  name           text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  title          text NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
  kind           text NOT NULL CHECK (kind IN ('upload', 'synced', 'live')),
  connection_id  uuid,
  config         jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(config) = 'object' AND octet_length(config::text) <= 8192),
  sync_every     interval NOT NULL DEFAULT '1 hour' CHECK (sync_every >= interval '15 minutes'),
  next_sync_at   timestamptz,                                              -- NULL: never (live)
  lease_until    timestamptz,
  status         text NOT NULL DEFAULT 'idle' CHECK (status IN ('idle', 'syncing', 'failed')),
  last_synced_at timestamptz,
  last_error     text CHECK (length(last_error) <= 500),
  embed_model    text,                                                     -- the model of its chunks' embeddings
  created_by     text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),   -- Actor.Ref()
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind = 'upload') = (connection_id IS NULL)),
  CHECK ((kind = 'live') = (next_sync_at IS NULL)),
  -- NO ACTION, checked at the statement's end: an organization's purge removes its connections
  -- and sources in one cascade in whatever order; alone, a connection a source reads cannot be
  -- deleted (ports.Connections.Delete: ErrConflict).
  FOREIGN KEY (org_id, connection_id) REFERENCES connections (org_id, id),
  UNIQUE (org_id, name),
  UNIQUE (org_id, id)
);
-- The claim: the sources of the organization due first. Live sources never sync.
CREATE INDEX knowledge_sources_due_idx ON knowledge_sources (org_id, next_sync_at) WHERE kind <> 'live';
-- A connection's deletion checks the sources that read it.
CREATE INDEX knowledge_sources_connection_idx ON knowledge_sources (org_id, connection_id) WHERE connection_id IS NOT NULL;

CREATE TABLE knowledge_documents (
  id          uuid PRIMARY KEY,                                            -- UUIDv7 from goald
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  source_id   uuid NOT NULL,
  external_id text NOT NULL CHECK (length(external_id) BETWEEN 1 AND 1024), -- a path, a page id, an upload's sha256
  version     text NOT NULL CHECK (length(version) <= 1024),                -- git blob id, page version, upload sha256
  title       text NOT NULL CHECK (length(title) <= 1024),
  url         text CHECK (length(url) <= 2048),                            -- synced: the link back
  mime        text NOT NULL CHECK (length(mime) <= 255),
  bytes       bigint NOT NULL CHECK (bytes >= 0),
  blob_sha256 text CHECK (blob_sha256 ~ '^[0-9a-f]{64}$'),                 -- uploads: orgs/<org-id>/knowledge/<sha256>
  status      text NOT NULL CHECK (status IN ('pending', 'indexed', 'failed', 'skipped')),
  error       text CHECK (length(error) <= 500),
  seen_at     timestamptz NOT NULL,
  indexed_at  timestamptz,
  embed_model text,
  created_by  text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),
  created_at  timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (org_id, source_id) REFERENCES knowledge_sources (org_id, id) ON DELETE CASCADE,
  UNIQUE (source_id, external_id),
  UNIQUE (org_id, source_id, id)
);
CREATE INDEX knowledge_documents_org_source_idx ON knowledge_documents (org_id, source_id, id);
-- Whether another document still holds an upload's content, before its object goes.
CREATE INDEX knowledge_documents_blob_idx ON knowledge_documents (org_id, blob_sha256) WHERE blob_sha256 IS NOT NULL;

CREATE TABLE knowledge_chunks (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  source_id   uuid NOT NULL,
  document_id uuid NOT NULL,
  ord         int NOT NULL CHECK (ord >= 0),
  heading     text NOT NULL DEFAULT '' CHECK (length(heading) <= 1000),
  text        text NOT NULL CHECK (length(text) <= 8000),
  tsv         tsvector GENERATED ALWAYS AS (to_tsvector('simple', heading || ' ' || text)) STORED,
  embed_model text,
  -- A chunk belongs to a document of its own source and organization.
  FOREIGN KEY (org_id, source_id, document_id) REFERENCES knowledge_documents (org_id, source_id, id) ON DELETE CASCADE,
  UNIQUE (document_id, ord)
);
CREATE INDEX knowledge_chunks_org_source_idx ON knowledge_chunks (org_id, source_id, document_id);
CREATE INDEX knowledge_chunks_tsv_idx ON knowledge_chunks USING gin (tsv);

-- Only where pgvector's extension exists: the embedding, its CHECK and its index, as the file had
-- them before its edit. The type and operator class are named in the extension's own schema.
-- +goose StatementBegin
DO $$
DECLARE
  ext text;
BEGIN
  SELECT n.nspname INTO ext FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
  WHERE e.extname = 'vector';
  IF ext IS NOT NULL THEN
    EXECUTE format('ALTER TABLE knowledge_chunks ADD COLUMN embedding %I.halfvec(1024), '
      || 'ADD CHECK ((embedding IS NULL) = (embed_model IS NULL))', ext);
    EXECUTE format('CREATE INDEX knowledge_chunks_embedding_idx ON knowledge_chunks '
      || 'USING hnsw (embedding %I.halfvec_cosine_ops)', ext);
  END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE knowledge_sources ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON knowledge_sources
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE knowledge_documents ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON knowledge_documents
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE knowledge_chunks ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON knowledge_chunks
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON knowledge_sources, knowledge_documents, knowledge_chunks TO goal_app;
GRANT UPDATE (title, config, sync_every, next_sync_at, lease_until, status, last_synced_at, last_error, embed_model, updated_at)
  ON knowledge_sources TO goal_app;
GRANT UPDATE (version, title, url, mime, bytes, blob_sha256, status, error, seen_at, indexed_at, embed_model)
  ON knowledge_documents TO goal_app;
