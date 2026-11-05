-- +goose Up
-- The latest repository test of each factory and repository, or owner pattern (ADR 0041
-- decision 8; ports.RepositoryTests): POST .../factories/{factory}/repositories/test asks the
-- code host what the token in a secret can do there, for a read or a write entry, and keeps one
-- row per factory and url, the newest replacing the one before. Replacing the secret's value
-- tests each row that names it again. url is the repository's lower-case host/owner/name, or
-- host/owner/* for an owner pattern. Every text is scrubbed of the token: a row holds no value.
-- Rows go with the factory or the organization (ON DELETE CASCADE), so goal_purge_org and
-- goal_delete_factory need no change.
CREATE TABLE repository_tests (
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id  uuid NOT NULL,
  url         text NOT NULL CHECK (url = lower(url) AND url ~ '^[^/\s]+(/[^/\s]+){2,}$' AND url NOT LIKE '%.git'),
  access      text NOT NULL CHECK (access IN ('read', 'write')),
  secret      text NOT NULL CHECK (secret ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  at          timestamptz NOT NULL,
  tested      boolean NOT NULL,
  ok          boolean NOT NULL,
  summary     text NOT NULL CHECK (char_length(summary) <= 2000),
  warning     text NOT NULL DEFAULT '' CHECK (char_length(warning) <= 2000),
  read_ok     boolean NOT NULL,
  read_reason text NOT NULL CHECK (char_length(read_reason) <= 2000),
  push_ok     boolean NOT NULL,
  push_reason text NOT NULL CHECK (char_length(push_reason) <= 2000),
  -- For an owner pattern: the repositories the token can read and push to, and whether the count
  -- stopped at the adapter's limit. Zero and false for one repository.
  readable    int NOT NULL DEFAULT 0 CHECK (readable >= 0),
  pushable    int NOT NULL DEFAULT 0 CHECK (pushable >= 0),
  truncated   boolean NOT NULL DEFAULT false,
  -- A test the host didn't answer can't pass.
  CHECK (tested OR NOT ok),
  PRIMARY KEY (org_id, factory_id, url),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);
-- The key serves one factory's rows; this index the rows a secret's replaced value tests again.
CREATE INDEX repository_tests_org_secret_idx ON repository_tests (org_id, secret);

ALTER TABLE repository_tests ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON repository_tests
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON repository_tests TO goal_app;
GRANT UPDATE (access, secret, at, tested, ok, summary, warning, read_ok, read_reason, push_ok, push_reason, readable, pushable, truncated)
  ON repository_tests TO goal_app;
