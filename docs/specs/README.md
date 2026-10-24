# Phase 2.2 specs

Phase 2.2 builds every backend and frontend piece the Goal UI canvas shows, with credentials, connectors and the MCP gateway last. Each phase has one spec here, written before any code; the specs are reviewed together, then each phase is built in small pull requests.

| Phase | Spec | Depends on |
| --- | --- | --- |
| A | [Organizations and identity](2.2-a-organizations.md) | — |
| B | [Definitions and versions](2.2-b-definitions.md) | A |
| C | [App shell and organization screens](2.2-c-app-shell.md) | A |
| D | [Factory builder](2.2-d-factory-builder.md) | B, C |
| E | [Work screens and flow metrics](2.2-e-work-screens.md) | A, C (B for `definition_version_id` on runs) (review) |
| F | [Credentials, connectors and MCP gateway](2.2-f-credentials-and-connectors.md) (two parts: F1 vault and credentials, F2 connectors and MCP gateway; a dedicated design pass comes first and may rewrite it) | A–E |

(restructure) Vault and credentials were phase C and connectors phase G. They are one last phase, F, because credentials exist for model access, repositories and connectors, and that whole area gets its own design pass before it is built. Phases are lettered in build order. Section references to F name the part: `F1 §6.6`, `F2 §4.3`; F's pull requests are `F1-1…F1-9` and `F2-1…F2-26`.

Cross-spec review, build order and open decisions: [review-2.2.md](review-2.2.md). (review)

## Shared conventions every spec follows

These keep the six specs from colliding. A spec that needs to break one says so in its "Cross-spec contracts" section.

### Decisions already made
- The organization is the tenant boundary. Every org-owned table carries `org_id uuid NOT NULL` referencing `organizations(id)`, with PostgreSQL row-level security: `USING/WITH CHECK (org_id = current_setting('goal.org_id')::uuid)`. goald connects as `goal_app` (no BYPASSRLS); migrations run as `goal_owner`. Each request or engine transaction runs `SET LOCAL goal.org_id`.
- Roles: organization `owner | admin | member`; factory `editor | approver | viewer`. Locally one default organization and one implicit user until OIDC (OIDC itself is specified in A but built last in A).
- Credentials belong to an organization and are granted to its factories (phase F). Working default for F's design pass: PostgreSQL vault with envelope encryption (AES-256-GCM data key per secret version, wrapped by a key-encryption key: local key file, AWS KMS in the cloud). (restructure)
- Definitions: file content in the bucket as immutable blobs `orgs/<org-id>/blobs/<sha256>`; versions in PostgreSQL (`definition_versions`: draft | published, manifest path→sha256, `factories.current_version_id`). Publishing is one transaction; readers are notified with NOTIFY.
- Navigation: organization mark atop a factory rail; factory sidebar Work / Build / Learn; organization pages with an org sidebar. The UI canvas is the visual source of truth.
- Nothing about a factory is code or an environment variable. Environment variables configure only the platform.
- **Interim rule until phase F (restructure).** Model access and repository access stay as they are today: platform-level secrets of the local install, resolved through the existing `ports.Secrets` env adapter (`envsecrets`, unchanged signature `Resolve(ctx, ref)`). Model access is goald's `GOAL_TASK_ENV` (default `CLAUDE_CODE_OAUTH_TOKEN=env:CLAUDE_CODE_OAUTH_TOKEN`); the repository token and GitHub MCP headers are `env:GOAL_GITHUB_TOKEN` references in the factory definition. This is a temporary exception to the rule above, removed in phase F, which introduces `credential:` references and moves these values into the vault. Until then every factory of the install shares these secrets, factory definitions keep their `env:` references (`spec.runtime.repository.token`, connection `headers`, `env`, command words), and phases A–E add no credential fields, grants or screens. What B validates before F (B §4.7): each `env:NAME` reference parses and names a variable that is set and non-empty in goald's environment (checked through `ports.Secrets`, value discarded; `secret-env-unset`), and is not one of goald's own settings such as `GOAL_DATABASE_URL` or `GOAL_GATEWAY_KEY` (`env-reserved`); a `credential:` reference is an error (`credentials-unavailable`, "credentials arrive with phase F"); literals keep B's `literal-secret` warning. No grant, expiry or purpose checks: there is nothing to grant before F. F1 plugs the vault into B's `authoring.Credentials` extension point. Every organization on the install can reference these variables, so an install with several organizations holds only secrets every one of them may use.

### Naming registry (use these names; add, don't rename)
- API base: `/v1/orgs/{org}/...` (org = slug or id). Factory routes: `/v1/orgs/{org}/factories/{factory}/...`. Items: `/v1/orgs/{org}/items/{item}` (item = UUID or `<factory>#<number>`). The existing unscoped `/v1/...` routes move under the org prefix; the CLI follows. Outside the org prefix only: `/v1/health`, `/v1/me`, `/v1/orgs`, `/v1/auth/*`, `/v1/invitations/accept` (A), `/v1/oauth/callback` and `/oauth/client-metadata.json` (F2). (review)
- Ports (internal/ports): existing `Store, Runner, Task, Workspaces, Checker, Sidecars, Secrets, Contracts, ObjectStore, ItemNotices, CodeHost, Clock`. New: `Identity`, `Tenancy`, `Audit`, `SignIn` (A); `DefinitionStore`, `DefinitionNotices` (B); `SkillSource` (D); `FlowStats`, optional `Task` interfaces `TranscriptSource` (later) and `TaskInput` (E); `Vault`, `KeyWrapper`, `ModelAuth`, `CredentialProbe` (F1); `ConnectorCatalog`, `OAuth`, `ToolApprovals`, `Minter`, `MCPRegistry` (F2). Until F, `Secrets` keeps today's signature (restructure). Every port method that touches org-owned rows reads the org from `ctx` (`domain.OrgFrom`, A); an explicit `org` parameter shown in a spec is dropped when the port is written. (review)
- Migration ranges (forward-only, goose): A `00004–00009` (`00004–00008` used, `00009` reserved), B `00010–00019` (`00010–00011` used; `00012` = drop `factories.definition*`, moved out of 2.2: it would run before E's `00030` backfill reads that column; `00013` reserved), E `00030–00039` (`00030–00033` used, `00034` reserved for transcripts, deferred), F `00040–00049` (F1 vault `00040–00041`, F2 connectors `00042–00044`). C and D add no tables. `00020–00029` is unused (it was the vault's range before the restructure); `00050+` for anything later. (review, restructure)
- OpenAPI: one tag per phase, two for F (`orgs`, `definitions`, `shell`, `builder`, `work`, then F1 `credentials` and F2 `connectors`; `credentials` was `vault`, restructure); schemas named in PascalCase, shared types defined once: `Org`, `Factory` (A; B adds `current`, `draft`, `paused`), `Item`, `ItemCard` (E adds flow fields, F adds `pending_approval`), `User`, `UserRef` (A), `Problem` (A adds `code`, B adds `problems[]` of `DefinitionProblem`), `AndonPull` (C defines, E extends), `InboxEntry` (C defines kind `decision`, E adds kinds, F2 adds `tool_approval` with F2's `ToolApproval`). Problem `code` values are kebab-case (`draft-conflict`, `credential-name-taken`); a spec showing snake_case codes means the kebab-case spelling. (review)
- Web routes: `/o/:org` (factories home), `/o/:org/f/:factory` (Floor), `/board`, `/items`, `/items/:ref`, `/items/:ref/approvals/:approval` (F2), `/definition/:section` (D; sections `basics|line|agents|connections|review|versions`), `/agents/:agent` (E owns route and frame; D and F add cards), `/connections` (F2); org pages `/o/:org/members`, `/catalog`, `/catalog/new`, `/catalog/:connector` (F2), `/credentials`, `/credentials/new` (F1), `/settings`; wizard `/o/:org/new/:step`; `/invite/:token` (A, OIDC). Org page route files live under C's pathless `o.$org._org` layout. (review)
- Query keys: root `['org', <org id>, …]` (C). Work keys under `['org', id, 'factory', f, <board|metrics|items|weekly|lead-time|constraint|andon|agent>]` (A, E); other roots under the org: `definition` keys stay at `['org', id, 'factory', f, 'draft'|'versions'|'compare'|…]` (B), `builder` (D), `inbox`, `attention`, `overview` (C/E), `credentials` (F1), `connectors`, `approvals` (F2). Stream invalidation names these children explicitly; it never invalidates the bare `['org', id, 'factory', f]` prefix on an item event. (review)
- Events on `/v1/orgs/{org}/stream` (C owns the event union and the client registry; A owns the server feed): `item`, `resync` (existing), `definition` (B), `factory` (E), `credential` (F1), `approval`, `connection` (F2). NOTIFY channels, every payload with `org_id`: `goal_events` (item events; E's factory notices), `goal_access` (A), `goal_definitions` (B), `goal_approvals` (F2), `goal_org_events` (A; carries F's `credential` and `connection` events). (review)
- Task token claims (`domain.TaskClaims`): existing `f i s a r c e`; `o` org (A), `v` definition version (B). F2's `g` owner-instance claim is deferred with multi-instance forwarding. (review)
- Bucket: `orgs/<org-id>/blobs/<sha256>` (B), `catalog/templates/` (B), `catalog/skills/` (D), `catalog/connectors/` (F2); all are definition content (ADR 0005 amendment lists all three catalogs). Nothing else goes in the bucket (AGENTS.md rule 3), so run transcripts are not stored there in 2.2. (review)
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
| `internal/builder` | D | `domain`, `ports`, `config` | `builder` rule (D §5.2) |
| `internal/adapters/skillssh` | D | own deps | adapters rule |
| `internal/secrets` | F1 | `domain`, `ports` | `secrets` rule (F1 §5.8) |
| `internal/adapters/keyfile`, `anthropic` | F1 | own deps | adapters rule |
| `internal/connectors` | F2 | `domain`, `ports`, `config`, `authoring` (to implement `authoring.Check`), `schema`, yaml, jsonschema | `connectors` rule (F2 §5.5) |
| `internal/adapters/oauthclient`, `awssts`, `mcpregistry` | F2 | own deps | adapters rule |

The `api` rule also denies `internal/authoring`, `factorymodel`, `secrets`, `builder` and `connectors` (handlers reach them through interfaces declared in `internal/api`). The `engine` rule should become an allow-list (`domain`, `ports` only), since a deny-list misses the new core packages. Each new row goes into `docs/engineering/architecture.md`'s import table in the PR that adds the package.

### Rules (binding, from AGENTS.md)
Config-driven; factories keyed by UUID; PostgreSQL is the only database; stateless goald; hexagonal architecture enforced by depguard; manual constructor injection in `cmd/*/main.go`; 12-factor config; Terraform only in `terraform/` and not in this phase; no employer names (use `acme`, `example.com`); UI follows `design-system/goal/MASTER.md`; tests protect behaviour; forward-only migrations; one work item, one small pull request.
