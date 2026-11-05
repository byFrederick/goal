-- +goose Up
-- Each item's repositories and its stored branch (ADR 0041 decisions 4 and 6).
--
-- work_item_repositories has one row per item and repository, keyed by the repository's
-- normalized host/owner/name in lower case (domain.RepositoryKey), never by an entry's name or
-- place, so renaming or reordering runtime.repositories never moves an item. joined says how it
-- joined (filing, default, push, output) and ordinal keeps the order, at most 10 per item
-- (domain.MaxItemRepositories). base_commit and integration_branch stay NULL until a later part
-- writes them. Rows are never removed by goald (no DELETE grant); they go with their item or
-- organization (ON DELETE CASCADE), so goal_purge_org and goal_delete_factory need no change.
CREATE TABLE work_item_repositories (
  org_id             uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  item_id            uuid NOT NULL,
  repo               text NOT NULL CHECK (repo = lower(repo) AND repo ~ '^[^/\s]+(/[^/\s]+){2,}$' AND repo NOT LIKE '%.git'),
  joined             text NOT NULL CHECK (joined IN ('filing', 'default', 'push', 'output')),
  joined_at          timestamptz NOT NULL,
  ordinal            int NOT NULL CHECK (ordinal >= 0 AND ordinal < 10),
  base_commit        text,
  integration_branch text,
  PRIMARY KEY (item_id, repo),
  UNIQUE (item_id, ordinal),
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE
);
-- The keys serve one item's rows; this index the items that use a repository.
CREATE INDEX work_item_repositories_org_repo_idx ON work_item_repositories (org_id, repo);

ALTER TABLE work_item_repositories ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON work_item_repositories
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON work_item_repositories TO goal_app;
GRANT UPDATE (base_commit, integration_branch) ON work_item_repositories TO goal_app;

-- The branch an item's changes live on, set once at filing to goal/<factory>/<number>-<slug>
-- (domain.ItemBranch) and never recomputed, so renaming the item or its factory never moves it.
-- NULL only for an item an instance from before this migration inserts during a deploy;
-- Item.Branch then falls back to goal/<number>-<slug>.
ALTER TABLE work_items ADD COLUMN branch text;

-- Every existing item keeps the branch it has today, goal/<number>-<slug>, computed as Go's
-- branchName does: the title lowercased in ASCII only (Go also lowercases U+0130 and U+212A to
-- i and k), each run of other characters one dash, no dash at either end, and the slug cut at
-- the first letter or digit that makes it 40 characters long, 41 when that letter follows a dash.
UPDATE work_items w SET branch = 'goal/' || w.number || CASE WHEN s.slug = '' THEN '' ELSE '-' || s.slug END
FROM (
  SELECT id, CASE
    WHEN length(full_slug) <= 40 THEN full_slug
    WHEN substr(full_slug, 40, 1) = '-' THEN left(full_slug, 41)
    ELSE left(full_slug, 40)
  END AS slug
  FROM (
    SELECT id, btrim(regexp_replace(
      lower(translate(title, U&'\0130\212A', 'IK') COLLATE "C"), '[^a-z0-9]+' COLLATE "C", '-', 'g'
    ), '-') AS full_slug
    FROM work_items
  ) t
) s
WHERE s.id = w.id;
