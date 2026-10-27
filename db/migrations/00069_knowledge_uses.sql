-- +goose Up
-- Knowledge use and verdicts (ADR 0027). A use is one row per run, document and kind: 'search'
-- when knowledge__search returned some of the document's chunks (their ids, merged by each later
-- search of the run), 'read' when knowledge__read opened it; at is the run's first use. A verdict
-- is 'helped' or 'misled' on one document by an actor (Actor.Ref(): 'user:<id>' or
-- 'agent:<name>'), optionally of a run and its item, with a note of at most 500 characters; one
-- per actor, document and run, a verdict without a run counting as its own run (NULLS NOT
-- DISTINCT), and a later one replaces it.
--
-- Both reference their document with its source and organization and cascade, so deleting a
-- document, its source (00054's cascade to documents) or the organization deletes them. The
-- factory, item and run are plain uuids with no key: uses are written best effort, outside the
-- run's transaction (decision 1). Neither table copies a document's text, snippet or title
-- (decision 6). goal_app inserts and updates what an upsert changes, and never deletes.
CREATE TABLE knowledge_uses (
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id  uuid NOT NULL,
  item_id     uuid NOT NULL,
  run_id      uuid NOT NULL,
  source_id   uuid NOT NULL,
  document_id uuid NOT NULL,
  kind        text NOT NULL CHECK (kind IN ('search', 'read')),
  chunk_ids   bigint[] NOT NULL DEFAULT '{}' CHECK (cardinality(chunk_ids) <= 1000),
  at          timestamptz NOT NULL,
  PRIMARY KEY (org_id, run_id, document_id, kind),
  FOREIGN KEY (org_id, source_id, document_id) REFERENCES knowledge_documents (org_id, source_id, id) ON DELETE CASCADE
);
-- A source's window (SourceUsage) and a document's runs, newest first (DocumentUses); the
-- primary key serves a run's uses (RunUses).
CREATE INDEX knowledge_uses_source_idx   ON knowledge_uses (org_id, source_id, at);
CREATE INDEX knowledge_uses_document_idx ON knowledge_uses (org_id, document_id, at DESC);

CREATE TABLE knowledge_verdicts (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  source_id   uuid NOT NULL,
  document_id uuid NOT NULL,
  run_id      uuid,
  item_id     uuid CHECK (item_id IS NULL OR run_id IS NOT NULL),
  verdict     text NOT NULL CHECK (verdict IN ('helped', 'misled')),
  note        text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
  actor       text NOT NULL CHECK (actor ~ '^(user|agent):.+$'),
  at          timestamptz NOT NULL,
  CONSTRAINT knowledge_verdicts_actor_key UNIQUE NULLS NOT DISTINCT (org_id, actor, document_id, run_id),
  FOREIGN KEY (org_id, source_id, document_id) REFERENCES knowledge_documents (org_id, source_id, id) ON DELETE CASCADE
);
-- A source's window (SourceUsage) and the document key's cascade.
CREATE INDEX knowledge_verdicts_source_idx   ON knowledge_verdicts (org_id, source_id, at);
CREATE INDEX knowledge_verdicts_document_idx ON knowledge_verdicts (org_id, document_id);

ALTER TABLE knowledge_uses ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON knowledge_uses
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
ALTER TABLE knowledge_verdicts ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON knowledge_verdicts
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON knowledge_uses, knowledge_verdicts TO goal_app;
GRANT UPDATE (chunk_ids, at) ON knowledge_uses TO goal_app;
GRANT UPDATE (item_id, verdict, note, at) ON knowledge_verdicts TO goal_app;
