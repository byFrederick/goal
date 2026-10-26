---
name: goal-migration
description: Change the PostgreSQL schema end to end, from a new numbered migration through the queries, sqlc, the Store port, its in-memory fake and the conformance suite. Use whenever a table, column, index, function or policy changes.
---

# Change the schema

The database rules (forward-only, row-level security, indexes, definers) are in [database.md](../../rules/database.md). What moves with a migration is the migration row of the [change map](../../../docs/engineering/change-map.md). Read [testing.md](../../../docs/engineering/testing.md) before the tests.

1. **Take the next number.** Find the current phase's range in the migration ranges of `docs/specs/README.md`. Then list `migrations/`. The new file is the highest number plus one, inside that range, named `NNNNN_what_it_does.sql`. Never edit or renumber an applied file.
2. **Write the migration** in `migrations/`. It opens with `-- +goose Up` and has no Down section, because migrations only go forward. Goose runs each file in one transaction. An organization-scoped table gets `org_id`, row-level security and an index that starts with `org_id`.
3. **Record the number.** Add the file to its phase's entry in the migration ranges of `docs/specs/README.md`.
4. **Write the queries** in `queries/`, then run `task sqlc`. It regenerates `internal/adapters/postgres/db`; never edit that directory by hand.
5. **Extend the port.** Add or change the method on the `Store` port (or the port that owns the data) in `internal/ports/ports.go`. Use domain types, never generated rows.
6. **Update the fake.** Make the in-memory fake in `internal/ports/porttest` behave as PostgreSQL will. Engine tests rely on it.
7. **Add the conformance case.** Put the behavior in the shared suite: `internal/ports/porttest/storetest` for the `Store`, the matching `*test` package for another port. The fake runs it in `internal/ports/porttest/store_test.go`, and PostgreSQL runs it in `internal/adapters/postgres/store_integration_test.go` (`TestStore_Conformance`).
8. **Implement the adapter** in `internal/adapters/postgres` over the generated queries.
9. **Run the checks.** `task test` (the fake's run of the suite) and `task lint`.
10. **Run the database checks.** `task migrate-check` applies every migration to a throwaway PostgreSQL 18 with pgvector. `task test-integration` runs the suite against PostgreSQL. Both need a PostgreSQL. Where none can be started, they still run in CI on the pull request: say in the pull request that you did not run them yourself.
11. **Docs as built.** Update the spec that describes the data.
12. **Deliver** with [goal-deliver](../goal-deliver/SKILL.md).
