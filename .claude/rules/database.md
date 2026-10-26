---
paths:
  - "db/migrations/**"
  - "db/queries/**"
  - "db/sqlc.yaml"
  - "internal/adapters/postgres/**"
---

# Database

The data model and its reasons: [docs/specs/README.md](../../docs/specs/README.md) ("Migration ranges" and the tenancy bullet), [architecture.md](../../docs/engineering/architecture.md) ("Ports", "Scale and failure") and [testing.md](../../docs/engineering/testing.md).

## Migrations

- Write migrations for goose, forward-only, as a new numbered file in `db/migrations/`, because goose applies them in order and refuses a number below one already applied.
- Never edit an applied migration; fix it with the next file, because every database that ran it keeps the old one.
- Take the next free number in your phase's range from docs/specs/README.md, so parallel work does not collide.
- Keep each migration safe in one transaction, and add a large table's new constraint `NOT VALID` then validate it, so deploys do not lock tables.
- Run `task migrate-check` after a change to `db/migrations/`, because it applies every file to a fresh PostgreSQL 18 with pgvector.

## Tenancy

- Give every org-owned table `org_id uuid NOT NULL` and a row-level security policy on `goal.org_id`, because goald connects as `goal_app` without BYPASSRLS.
- Start an index of every org-owned table with `org_id`, so scoped queries stay fast (`TestSchema_EveryOrgTableHasAnIndexOnOrgID` checks it).
- Pin a `SECURITY DEFINER` function's `search_path` to its schema with `pg_temp` last, so a caller cannot shadow its objects.

## Queries

- Put SQL in `db/queries/` and regenerate with `task sqlc`, because `internal/adapters/postgres/db` is generated and never edited by hand.
- Run each store method in one transaction and lock an item before its lease or runs, so concurrent methods cannot deadlock.

## Tests

- Add new store behavior to the conformance suite in `internal/ports/porttest/storetest` (or the port's own suite), so the fake and PostgreSQL agree.
- Run the adapter's integration tests, bounded: `go test -race -tags integration -run '<tests>' -timeout 10m ./internal/adapters/postgres/` with `GOAL_TEST_DATABASE_URL` set.
- Expect CI to run `task test-integration` and `task migrate-check`; run them locally when Docker is there.

## Skills

Load `supabase-postgres-best-practices`, `postgresql-table-design`, `postgresql-optimization`, `postgresql-code-review`, `sql-code-review`, `database-migration` and `golang-database`.
