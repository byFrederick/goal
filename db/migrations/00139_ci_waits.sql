-- +goose Up
-- CI waits (ADR 0044 decisions 4, 7 and 9). A CI wait is a row instead of an agent idling in a
-- run box: ci_waits has one row per item, station visit and repository, and ci_polls one row per
-- code host, repository and commit, shared by every wait on that commit, which goald polls under a
-- lease so instances share the polling and a restart resumes it.
--
-- A poll is keyed by organization as well: each organization reads its code host with its own
-- token, and row-level security scopes both tables to one organization. visit is an opaque key
-- the caller supplies for one visit of the item to the station. checks and held are opaque JSON
-- the poller and the gates decode (domain.CIPoll.Checks, domain.CIWait.Held).
--
-- goald never removes rows (no DELETE grant); waits go with their item and polls with their
-- organization (ON DELETE CASCADE), so the purges need no change.
CREATE TABLE ci_polls (
  org_id              uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  host                text NOT NULL CHECK (host <> ''),
  repo                text NOT NULL CHECK (repo = lower(repo) AND repo ~ '^[^/\s]+(/[^/\s]+){2,}$' AND repo NOT LIKE '%.git'),
  sha                 text NOT NULL CHECK (sha <> ''),
  next_poll_at        timestamptz NOT NULL,
  -- The wait between polls: 15 s at first, rising to 2 min.
  interval_seconds    integer NOT NULL DEFAULT 15 CHECK (interval_seconds BETWEEN 15 AND 120),
  etag                text NOT NULL DEFAULT '',
  -- The last CommitChecks read, as JSON; NULL until a poll reads any.
  checks              jsonb,
  first_check_seen_at timestamptz,
  last_error          text NOT NULL DEFAULT '',
  lease_owner         text,
  lease_until         timestamptz,
  PRIMARY KEY (org_id, host, repo, sha),
  CHECK ((lease_owner IS NULL) = (lease_until IS NULL))
);

-- Due polls in time order, for LeaseDueCIPolls.
CREATE INDEX ci_polls_due_idx ON ci_polls (org_id, next_poll_at);

ALTER TABLE ci_polls ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON ci_polls
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON ci_polls TO goal_app;
GRANT UPDATE (next_poll_at, interval_seconds, etag, checks, first_check_seen_at, last_error, lease_owner, lease_until)
  ON ci_polls TO goal_app;

CREATE TABLE ci_waits (
  id         uuid NOT NULL PRIMARY KEY,
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  item_id    uuid NOT NULL,
  station    text NOT NULL CHECK (station <> ''),
  visit      text NOT NULL CHECK (visit <> ''),
  host       text NOT NULL,
  repo       text NOT NULL,
  sha        text NOT NULL,
  pr_url     text,
  -- before: CI on the item's commits before a run; after: CI on what a run pushed.
  phase      text NOT NULL CHECK (phase IN ('before', 'after')),
  round      integer NOT NULL CHECK (round >= 1),
  -- The outcome to apply when CI passes; only the after phase holds one.
  held       jsonb CHECK (held IS NULL OR phase = 'after'),
  started_at timestamptz NOT NULL,
  ended_at   timestamptz,
  outcome    text CHECK (outcome IN ('passed', 'failed', 'timed_out', 'no_ci', 'unreadable', 'superseded')),
  CHECK ((ended_at IS NULL) = (outcome IS NULL)),
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, host, repo, sha) REFERENCES ci_polls (org_id, host, repo, sha) ON DELETE CASCADE
);

-- One open wait per item, station visit and repository.
CREATE UNIQUE INDEX ci_waits_open_key ON ci_waits (item_id, station, visit, repo) WHERE ended_at IS NULL;
-- An item's waits, for OpenCIWaits and CIRounds.
CREATE INDEX ci_waits_org_item_idx ON ci_waits (org_id, item_id, station, visit);
-- A poll's open waits, for LeaseDueCIPolls' EXISTS.
CREATE INDEX ci_waits_open_poll_idx ON ci_waits (org_id, host, repo, sha) WHERE ended_at IS NULL;

ALTER TABLE ci_waits ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON ci_waits
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON ci_waits TO goal_app;
GRANT UPDATE (ended_at, outcome) ON ci_waits TO goal_app;
