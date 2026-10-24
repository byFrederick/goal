-- +goose Up
-- Review 2.2 (Go) Medium 2: the legacy import reads the bucket's pre-2.2 layout only for the
-- organizations that existed when versions arrived, and only until their import ends. A row is
-- an organization whose import is still to do: this migration adds one for every organization
-- there is, goald deletes it once the organization's import ran (DefinitionStore.EndLegacyImport),
-- and nothing ever adds one again. An organization created later never reads a legacy prefix,
-- and files written there afterwards are never read, nor is a deleted imported draft imported
-- again. Purging an organization cascades to its row.
CREATE TABLE legacy_import_pending (
  org_id uuid PRIMARY KEY DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE
);

INSERT INTO legacy_import_pending (org_id) SELECT id FROM organizations;

ALTER TABLE legacy_import_pending ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON legacy_import_pending
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

-- goald reads whether its organization's import is pending and ends it; it never adds a row.
GRANT SELECT, DELETE ON legacy_import_pending TO goal_app;
