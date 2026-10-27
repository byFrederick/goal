# ADR 0020 — Saved views and CSV exports

Status: accepted · 2026-10-27 · Builds on [ADR 0016](0016-ui-revamp.md) (the screens are the contract, controls for features not built yet) and [ADR 0005](0005-factory-definitions-in-object-storage.md) (the definition and the bucket hold only a factory's files)

## Context

The revamped screens draw three controls this ADR covers, and none of them is built yet. The [screens' index](../design/screens/README.md) lists them as "Not built yet":

- **Save view** on the Items page ([`daily-work/items.html`](../design/screens/daily-work/items.html), line 174, "+ Save view"). The Items page already keeps its filters in the URL search params. Today a person can only bookmark that URL, and there is no shared list of views for a factory.
- **Export CSV** on the Runs screen ([`daily-work/runs.html`](../design/screens/daily-work/runs.html), line 152) and on the Spend screen ([`organization/spend.html`](../design/screens/organization/spend.html), line 154). The API answers those lists only as paged JSON.

Several parts of assembly software#270 build these features at the same time: the store, the API and the UI for views, and the API and the UI for exports. They need one table shape, one set of permission rules and one CSV format to build against. These are the forces:

- A view is per-organization data that people change. It is not part of a factory's line. [AGENTS.md](../../AGENTS.md) rules 1 and 3 keep data like this out of the definition and out of the bucket.
- [`contracts/api/permissions.yaml`](../../contracts/api/permissions.yaml) already has the levels a view needs. `factory:read` is the viewer level and allows API keys and personal tokens (`keys: true, pat: true`). `factory:manage` is the editor level and allows API keys (`keys: true`). Both are scoped to a factory.
- Every operation in [`contracts/api/openapi.yaml`](../../contracts/api/openapi.yaml) names its permission in `x-goal-scope` (line 14), and every error is an RFC 9457 problem with a stable type, such as `api-key-name-taken` (409) and `too-large` (413).
- A CSV of every run in an organization can be large. Buffering it in goald's memory would tie one request's cost to the size of the organization.

## Decision

1. **A saved view is a name plus the Items page's URL search params, stored in PostgreSQL.** The filters are stored as a map from a string key to a list of strings (`map<string, list<string>>`), so a view reopens exactly the URL it was saved from. A view holds at most 20 keys, at most 20 values per key and at most 200 characters per value; the API rejects a larger one. Views live in a table `saved_views`, keyed by UUID, with these columns: `org_id`, `factory_id`, the owner's user id, `name` (1 to 60 characters), `shared` (a boolean), `filters` (`jsonb`), `created_at` and `updated_at`. The table has row-level security by organization, like every org-scoped table ([database rule](../../.claude/rules/database.md)). Views never go into the factory definition or the bucket (AGENTS.md rules 1 and 3).

2. **Any reader manages their own views; only an editor manages shared ones.** A member with `factory:read` on the factory can list the factory's shared views and their own personal views, and can create, rename and delete their own personal views. They never see another member's personal views. Creating, renaming or deleting a shared view needs `factory:manage` (editor). Only a signed-in person can own a view, so every write from an API key or a personal token is answered 403, even though `factory:manage` allows keys. The view API checks the caller's kind as well as the permission. Listing works for keys and tokens, which have no personal views and see only the shared ones. This adds no new permission to `contracts/api/permissions.yaml`. A personal view's name is unique for its owner, and a shared view's name is unique within its factory. A clash is answered 409 with the problem type `view-name-taken`.

3. **A CSV export is a streamed GET beside the list it exports.** An export is a `GET` operation at `<list path>/export`. It takes the same query filters and the same `x-goal-scope` as the list it exports, and ignores paging. It answers `200` with `Content-Type: text/csv; charset=utf-8` and `Content-Disposition: attachment; filename="<what>-<yyyy-mm-dd>.csv"`. The body is streamed row by row from the store with `encoding/csv` and is never buffered whole. Its first row is a header of snake_case column names that match the list's JSON fields. Quoting follows RFC 4180. Timestamps are RFC 3339 in UTC, and money is decimal USD. An export holds at most 100000 rows. Above that it is answered `413` with the problem type `export-too-large`, and this is checked before any byte of the body is written. Every error before the first row is an RFC 9457 problem. After the first row is written, the status cannot change any more, so an error then ends the stream early.

## Consequences

- One person's views follow them across browsers, and an editor can publish views the whole factory shares, without changing the definition.
- Exports reuse each list's filters and permission, so an export can never show more than its list shows. Each new export is one more operation in `contracts/api/openapi.yaml`, with nothing new to decide.
- The 100000-row cap and the check before the first byte mean a client gets either a whole file or a problem. A larger export has to be narrowed with filters.
- The parts of assembly software#270 build these decisions:
  - **views-store** (software#286) adds the `saved_views` migration with its RLS policy, the queries and the `Store` port methods (decision 1).
  - **views-api** (software#287) adds the views operations to `contracts/api/openapi.yaml`, with the permission and caller checks and `view-name-taken` (decisions 1 and 2).
  - **exports-api** (software#289) adds the `/export` operations for runs and spend (decision 3).
  - **views-ui** (software#290) enables Save view on the Items page, with personal and shared views (decisions 1 and 2).
  - **exports-ui** (software#292) enables Export CSV on the Runs and Spend screens (decision 3).
- This ADR does not cover Set lane, the other control in the same assembly.
