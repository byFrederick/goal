-- +goose Up
-- Each run's repositories (ADR 0041 decisions 5 and 17). agent_run_repositories has one row per
-- run and repository the run fetched or pushed, keyed by the repository's normalized
-- host/owner/name in lower case (domain.RepositoryKey), with base_commit and branch_head, the
-- base's and the item branch's commits before prepare brought the branch up to date, pushed_head,
-- the branch's commit on the remote when the agent ended, wrote, whether the run pushed a change,
-- and seq, the order the rows were recorded in. It replaces agent_runs' single branch_head,
-- base_head and pushed_head (00085). A Jonah try reruns a run from its rows (ADR 0039 decision 9).
--
-- repo is NULL only for a row this migration moved from a run whose item had no
-- work_item_repositories row (every item filed before 00130): repository URLs live only in the
-- definitions in the bucket (ADR 0041 decision 4, "Old items"), so a NULL repo means the
-- repository entry the run's factory version named, as the run used it. UNIQUE NULLS NOT DISTINCT
-- keeps that to one row per run.
--
-- Rows are never removed by goald (no DELETE grant); they go with their run or organization (ON
-- DELETE CASCADE), so the purges need no change.
CREATE TABLE agent_run_repositories (
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  run_id      uuid NOT NULL,
  repo        text CHECK (repo = lower(repo) AND repo ~ '^[^/\s]+(/[^/\s]+){2,}$' AND repo NOT LIKE '%.git'),
  base_commit text,
  branch_head text,
  pushed_head text,
  wrote       boolean NOT NULL DEFAULT false,
  -- seq orders a run's rows as they were recorded: the first is the run's /workspace.
  seq         bigint GENERATED ALWAYS AS IDENTITY,
  UNIQUE NULLS NOT DISTINCT (run_id, repo),
  FOREIGN KEY (org_id, run_id) REFERENCES agent_runs (org_id, id) ON DELETE CASCADE
);

ALTER TABLE agent_run_repositories ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON agent_run_repositories
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON agent_run_repositories TO goal_app;
GRANT UPDATE (base_commit, branch_head, pushed_head, wrote) ON agent_run_repositories TO goal_app;

-- Every run with a head keeps it, on its item's first repository when the item has one. A run
-- wrote when it pushed a commit other than the one it started from; a retry, which recorded no
-- branch head, wrote when it pushed at all.
INSERT INTO agent_run_repositories (org_id, run_id, repo, base_commit, branch_head, pushed_head, wrote)
SELECT r.org_id, r.id, w.repo, r.base_head, r.branch_head, r.pushed_head,
       r.pushed_head IS NOT NULL AND r.pushed_head IS DISTINCT FROM r.branch_head
FROM agent_runs r
LEFT JOIN work_item_repositories w ON w.item_id = r.item_id AND w.ordinal = 0
WHERE r.branch_head IS NOT NULL OR r.base_head IS NOT NULL OR r.pushed_head IS NOT NULL;

-- A try target is a finished station run that recorded a branch head (00102); the index keeps
-- its order without the dropped column, and the join to the rows filters.
DROP INDEX agent_runs_try_target_idx;
ALTER TABLE agent_runs DROP COLUMN branch_head, DROP COLUMN base_head, DROP COLUMN pushed_head;
CREATE INDEX agent_runs_try_target_idx ON agent_runs (org_id, item_id, station, started_at DESC)
  WHERE kind = 'station';
-- An organization's rows by run, for scoped reads (TestSchema_EveryOrgTableHasAnIndexOnOrgID);
-- the unique key serves one run's rows and the try readers' EXISTS.
CREATE INDEX agent_run_repositories_org_run_idx ON agent_run_repositories (org_id, run_id);
