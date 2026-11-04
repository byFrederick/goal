-- +goose Up
-- Kept tests run as tries (ADR 0039 decision 12). A kept test is tried where a change reaches its
-- station: on every proposal that reaches it (one try, three for a model proposal), and on the
-- open draft when the publish dialog opens. A publish's try has no proposal, so it keeps what it
-- runs on itself: version_id with edits (whole files of the draft, as jonah_proposals.edits holds
-- them, 00101). Every kept-test try records content_key, the SHA-256 of the manifest it runs on
-- (domain.ContentKey): a publish whose draft matches a tried proposal's content, or an earlier
-- publish's, reuses its tries instead of queuing more.
--
-- A kept-test try finishes graded: fixed when it passed the station's contract and checks and, at
-- a gate, approved; not_fixed otherwise; error when it could not be carried out. A try with no
-- proposal is claimed before a proposal's (ClaimTry, tries_due_idx).
--
-- kept_test_id cascades, so removing a kept test removes its tries; goal_purge_org (00042) and
-- goal_delete_factory (00061) need no change, as for 00102 and 00105. goal_app inserts the new
-- columns and never updates them.
ALTER TABLE kept_tests ADD CONSTRAINT kept_tests_org_id_key UNIQUE (org_id, id);

ALTER TABLE tries ALTER COLUMN proposal_id DROP NOT NULL;
ALTER TABLE tries ADD COLUMN kept_test_id uuid;
ALTER TABLE tries ADD COLUMN content_key text CHECK (length(content_key) BETWEEN 1 AND 100);
ALTER TABLE tries ADD COLUMN version_id uuid;
ALTER TABLE tries ADD COLUMN edits jsonb CHECK (jsonb_typeof(edits) = 'array');

ALTER TABLE tries ADD CONSTRAINT tries_kept_test_fkey
  FOREIGN KEY (org_id, kept_test_id) REFERENCES kept_tests (org_id, id) ON DELETE CASCADE;
ALTER TABLE tries ADD CONSTRAINT tries_subject_check CHECK (proposal_id IS NOT NULL OR kept_test_id IS NOT NULL);
ALTER TABLE tries ADD CONSTRAINT tries_publish_check
  CHECK (proposal_id IS NOT NULL OR (version_id IS NOT NULL AND edits IS NOT NULL AND content_key IS NOT NULL));
ALTER TABLE tries ADD CONSTRAINT tries_kept_test_key_check CHECK (kept_test_id IS NULL OR content_key IS NOT NULL);

-- A proposal's tries: one per item, kept test and attempt. A kept-test try of a proposal and its
-- cited item's try may share the item. A publish's tries have their own key below.
ALTER TABLE tries DROP CONSTRAINT tries_key;
CREATE UNIQUE INDEX tries_key ON tries (org_id, proposal_id, item_id, kept_test_id, attempt) NULLS NOT DISTINCT
  WHERE proposal_id IS NOT NULL;

-- A publish's tries: one per kept test, content and attempt, so queuing them is idempotent.
CREATE UNIQUE INDEX tries_publish_key ON tries (org_id, kept_test_id, content_key, attempt) WHERE proposal_id IS NULL;

-- A factory's kept-test tries by content (the reuse, and a publish's results), and a kept test's
-- newest finished one (its last result).
CREATE INDEX tries_content_idx ON tries (org_id, factory_id, content_key) WHERE kept_test_id IS NOT NULL;
CREATE INDEX tries_kept_test_idx ON tries (org_id, kept_test_id, finished_at DESC) WHERE kept_test_id IS NOT NULL;

-- The claim takes a factory's publish tries first, then the oldest.
DROP INDEX tries_due_idx;
CREATE INDEX tries_due_idx ON tries (org_id, factory_id, (proposal_id IS NOT NULL), created_at, id) WHERE state IN ('queued', 'running');
