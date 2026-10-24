# Phase 2.2 specs

Phase 2.2 builds every backend and frontend piece the Goal UI canvas shows, connectors and the MCP gateway last. Each phase has one spec here, written before any code; the specs are reviewed together, then each phase is built in small pull requests.

| Phase | Spec | Depends on |
| --- | --- | --- |
| A | [Organizations and identity](2.2-a-organizations.md) | — |
| B | [Definitions and versions](2.2-b-definitions.md) | A |
| C | [Vault and credentials (core)](2.2-c-vault.md) | A |
| D | [App shell and organization screens](2.2-d-app-shell.md) | A |
| E | [Factory builder](2.2-e-factory-builder.md) | B, C, D |
| F | [Work screens and flow metrics](2.2-f-work-screens.md) | A, D (B for `definition_version_id` on runs) (review) |
| G | [Connectors and MCP gateway](2.2-g-connectors.md) | C, E, F |

Cross-spec review, build order and open decisions: [review-2.2.md](review-2.2.md). (review)

## Shared conventions every spec follows

These keep the seven specs from colliding. A spec that needs to break one says so in its "Cross-spec contracts" section.

### Decisions already made
- The organization is the tenant boundary. Every org-owned table carries `org_id uuid NOT NULL` referencing `organizations(id)`, with PostgreSQL row-level security: `USING/WITH CHECK (org_id = current_setting('goal.org_id')::uuid)`. goald connects as `goal_app` (no BYPASSRLS); migrations run as `goal_owner`. Each request or engine transaction runs `SET LOCAL goal.org_id`.
- Roles: organization `owner | admin | member`; factory `editor | approver | viewer`. Locally one default organization and one implicit user until OIDC (OIDC itself is specified in A but built last in A).
- Credentials belong to an organization and are granted to its factories. Default vault: PostgreSQL, envelope encryption (AES-256-GCM data key per secret version, wrapped by a key-encryption key: local key file, AWS KMS in the cloud).
- Definitions: file content in the bucket as immutable blobs `orgs/<org-id>/blobs/<sha256>`; versions in PostgreSQL (`definition_versions`: draft | published, manifest path→sha256, `factories.current_version_id`). Publishing is one transaction; readers are notified with NOTIFY.
- Navigation: organization mark atop a factory rail; factory sidebar Work / Build / Learn; organization pages with an org sidebar. The UI canvas is the visual source of truth.
- Nothing about a factory is code or an environment variable. Environment variables configure only the platform.

### Naming registry (use these names; add, don't rename)
- API base: `/v1/orgs/{org}/...` (org = slug or id). Factory routes: `/v1/orgs/{org}/factories/{factory}/...`. Items: `/v1/orgs/{org}/items/{item}` (item = UUID or `<factory>#<number>`). The existing unscoped `/v1/...` routes move under the org prefix; the CLI follows. Outside the org prefix only: `/v1/health`, `/v1/me`, `/v1/orgs`, `/v1/auth/*`, `/v1/invitations/accept` (A), `/v1/oauth/callback` and `/oauth/client-metadata.json` (G). (review)
- Ports (internal/ports): existing `Store, Runner, Task, Workspaces, Checker, Sidecars, Secrets, Contracts, ObjectStore, ItemNotices, CodeHost, Clock`. New: `Identity`, `Tenancy`, `Audit`, `SignIn` (A); `DefinitionStore`, `DefinitionNotices` (B); `Vault`, `KeyWrapper`, `ModelAuth`, `CredentialProbe` (C); `SkillSource` (E); `FlowStats`, optional `Task` interfaces `TranscriptSource` (later) and `TaskInput` (F); `ConnectorCatalog`, `OAuth`, `ToolApprovals`, `Minter`, `MCPRegistry` (G). Every port method that touches org-owned rows reads the org from `ctx` (`domain.OrgFrom`, A); an explicit `org` parameter shown in a spec is dropped when the port is written. (review)
- Migration numbers in use (forward-only, goose): A `00004–00008` (`00009` reserved), B `00010–00011` (`00012` = drop `factories.definition*`, moved out of 2.2: it would run before F's `00030` backfill reads that column; `00013` reserved), C `00020–00021`, F `00030–00033` (`00034` reserved for transcripts, deferred), G `00040–00042`. D and E add none (`00050+` if they ever do). (review)
- OpenAPI: one tag per phase (`orgs`, `definitions`, `vault`, `shell`, `builder`, `work`, `connectors`); schemas named in PascalCase, shared types defined once: `Org`, `Factory` (A; B adds `current`, `draft`, `paused`), `Item`, `ItemCard` (F adds flow fields, G adds `pending_approval`), `User`, `UserRef` (A), `Problem` (A adds `code`, B adds `problems[]` of `DefinitionProblem`), `AndonPull` (D defines, F extends), `InboxEntry` (D defines kind `decision`, F adds kinds, G adds `tool_approval` with G's `ToolApproval`). Problem `code` values are kebab-case (`draft-conflict`, `credential-name-taken`); a spec showing snake_case codes means the kebab-case spelling. (review)
- Web routes: `/o/:org` (factories home), `/o/:org/f/:factory` (Floor), `/board`, `/items`, `/items/:ref`, `/items/:ref/approvals/:approval` (G), `/definition/:section` (E; sections `basics|line|agents|connections|review|versions`), `/agents/:agent` (F owns route and frame; E and G add cards), `/connections` (G); org pages `/o/:org/members`, `/catalog`, `/catalog/new`, `/catalog/:connector` (G), `/credentials`, `/credentials/new` (C), `/settings`; wizard `/o/:org/new/:step`; `/invite/:token` (A, OIDC). Org page route files live under D's pathless `o.$org._org` layout. (review)
- Query keys: root `['org', <org id>, …]` (D). Work keys under `['org', id, 'factory', f, <board|metrics|items|weekly|lead-time|constraint|andon|agent>]` (A, F); other roots under the org: `definition` keys stay at `['org', id, 'factory', f, 'draft'|'versions'|'compare'|…]` (B), `credentials` (C), `builder` (E), `inbox`, `attention`, `overview` (D/F), `connectors`, `approvals` (G). Stream invalidation names these children explicitly; it never invalidates the bare `['org', id, 'factory', f]` prefix on an item event. (review)
- Events on `/v1/orgs/{org}/stream` (D owns the event union and the client registry; A owns the server feed): `item`, `resync` (existing), `definition` (B), `credential` (C), `factory` (F), `approval`, `connection` (G). NOTIFY channels, every payload with `org_id`: `goal_events` (item events; F's factory notices), `goal_access` (A), `goal_definitions` (B), `goal_approvals` (G), `goal_org_events` (A; carries C's `credential` and G's `connection` events). (review)
- Task token claims (`domain.TaskClaims`): existing `f i s a r c e`; `o` org (A), `v` definition version (B). G's `g` owner-instance claim is deferred with multi-instance forwarding. (review)
- Bucket: `orgs/<org-id>/blobs/<sha256>` (B), `catalog/templates/` (B), `catalog/skills/` (E), `catalog/connectors/` (G); all are definition content (ADR 0005 amendment lists all three catalogs). Nothing else goes in the bucket (AGENTS.md rule 3), so run transcripts are not stored there in 2.2. (review)
- Permissions: only A's table (`internal/domain/permission.go`, A §11.2) maps roles to rights; other specs name a permission, never a role. (review)
- Audit actions match `^[a-z_]+\.[a-z_]+$`, noun then past-tense verb (`andon.pulled`, `run.transcript_read`). (review)

### Spec template (every spec has these sections, in this order)
1. Summary — what ships, in three sentences.
2. Scope — in / out, explicitly.
3. Canvas boards covered — exact board names from `project/canvas.json`, and any board element this phase does not deliver.
4. Domain and engine changes.
5. Ports and adapters — new or changed interfaces with Go signatures; which package implements each; depguard rules to add.
6. Data model — migrations (tables, columns, types, constraints, indexes, RLS policy, backfill), sqlc queries, conformance-suite additions.
7. Bucket layout changes (if any).
8. API — every endpoint: method, path, request/response schemas, errors (RFC 9457), required role, pagination; SSE events.
9. CLI changes.
10. Web UI — routes, components, data fetching (TanStack Query keys, SSE invalidation), states (loading, empty, error), accessibility, phone layout; mapped to canvas boards.
11. Security and tenancy — how isolation and roles are enforced; secrets handling; audit entries written.
12. Observability — logs, metrics, events.
13. Testing — unit tests with port fakes, conformance suite, integration tests (PostgreSQL 18, RustFS), Playwright flows; no useless tests.
14. Rollout and migration — from today's state (single tenant, env secrets) to the new one without losing data.
15. Work breakdown — ordered, small pull requests, each with its acceptance criteria and how it is verified.
16. Cross-spec contracts — what this phase provides to and needs from other phases (by interface name, endpoint or table).
17. Open questions.

### New packages and their depguard rules (review)
| Package | Phase | May import | Rule |
| --- | --- | --- | --- |
| `internal/adapters/oidc` | A | `go-oidc`, `oauth2` (only this package) | adapters rule |
| `internal/factorymodel`, `internal/authoring` | B | `domain`, `ports`, `config`, yaml | `definitions` rule + `authoring` rule (B §5.4) |
| `internal/secrets` | C | `domain`, `ports` | `secrets` rule (C §5.8) |
| `internal/adapters/keyfile`, `anthropic` | C | own deps | adapters rule |
| `internal/builder` | E | `domain`, `ports`, `config` | `builder` rule (E §5.2) |
| `internal/adapters/skillssh` | E | own deps | adapters rule |
| `internal/connectors` | G | `domain`, `ports`, `config`, `authoring` (to implement `authoring.Check`), `schema`, yaml, jsonschema | `connectors` rule (G §5.5) |
| `internal/adapters/oauthclient`, `awssts`, `mcpregistry` | G | own deps | adapters rule |

The `api` rule also denies `internal/authoring`, `factorymodel`, `secrets`, `builder` and `connectors` (handlers reach them through interfaces declared in `internal/api`). The `engine` rule should become an allow-list (`domain`, `ports` only), since a deny-list misses the new core packages. Each new row goes into `docs/engineering/architecture.md`'s import table in the PR that adds the package.

### Rules (binding, from AGENTS.md)
Config-driven; factories keyed by UUID; PostgreSQL is the only database; stateless goald; hexagonal architecture enforced by depguard; manual constructor injection in `cmd/*/main.go`; 12-factor config; Terraform only in `terraform/` and not in this phase; no employer names (use `acme`, `example.com`); UI follows `design-system/goal/MASTER.md`; tests protect behaviour; forward-only migrations; one work item, one small pull request.
