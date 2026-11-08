-- +goose Up
-- The index of per-repository build caches (ADR 0045). A row names one bucket object holding a
-- repository's Go module and build caches and pnpm store, keyed by a hash of its inputs
-- (cache_key). repository_key is a hash of the repository's clone URL, so every factory of an
-- organization shares a repository's caches.
--
-- One box reserves a key (state saving), writes the object and commits it (state ready). A saving
-- row older than an hour may be reserved again; a ready row never is. Pruning deletes rows and
-- hands their object keys to the caller, which deletes the objects. Rows go with their
-- organization (ON DELETE CASCADE).
CREATE TABLE build_caches (
  org_id         uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  repository_key text NOT NULL CHECK (repository_key <> ''),
  cache_key      text NOT NULL CHECK (cache_key ~ '^[0-9a-f]{64}$'),
  object_key     text NOT NULL CHECK (object_key <> ''),
  -- NULL while saving; the object's size once ready.
  size_bytes     bigint,
  state          text NOT NULL CHECK (state IN ('saving', 'ready')),
  created_at     timestamptz NOT NULL,
  used_at        timestamptz NOT NULL,
  UNIQUE (org_id, repository_key, cache_key),
  CHECK ((state = 'saving' AND size_bytes IS NULL) OR (state = 'ready' AND size_bytes IS NOT NULL AND size_bytes >= 0))
);

-- A repository's ready caches newest first, for PruneBuildCaches.
CREATE INDEX build_caches_ready_idx ON build_caches (org_id, repository_key, created_at DESC) WHERE state = 'ready';

ALTER TABLE build_caches ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON build_caches
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON build_caches TO goal_app;
GRANT UPDATE (state, size_bytes, object_key, created_at, used_at) ON build_caches TO goal_app;
