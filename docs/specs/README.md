# Phase 2.2 specs

Phase 2.2 builds every backend and frontend piece of the Goal UI canvas that needs neither credentials nor identity: organizations and tenancy, versioned definitions, the app shell, the factory builder and the work screens. Each phase has one spec here, written before any code; the specs are reviewed together, then each phase is built in small pull requests.

| Phase | Spec | Depends on |
| --- | --- | --- |
| A | [Organizations and tenancy](2.2-a-organizations.md) | — |
| B | [Definitions and versions](2.2-b-definitions.md) | A |
| C | [App shell and organization screens](2.2-c-app-shell.md) | A |
| D | [Factory builder](2.2-d-factory-builder.md) | B, C |
| E | [Work screens and flow metrics](2.2-e-work-screens.md) | A, C (B for `definition_version_id` on runs) (review) |

### Later phases

| Phase | Spec | Notes |
| --- | --- | --- |
| 3 | [Identity and access](3-identity-and-access.md) | After 2.2. Better Auth beside goald ([ADR 0007](../adr/0007-identity-better-auth.md)); takes everything in "Out of 2.2" below. PRs `3-1…3-16`, migrations `00040–00049`. |
| 4 | [Credentials, connectors and MCP gateway](4-credentials-and-connectors.md) (part 4.1 vault and credentials, part 4.2 connectors and MCP gateway) | After phase 3. A design pass comes first and may rewrite the spec. Migrations `00050–00059`. |
| 5 | [Goal MCP server](5-goal-mcp-server.md) (stub) | After phase 4: Goal itself as an MCP server for outside clients, OAuth through Better Auth. Migrations from `00060`. |

The roadmap stops at phase 5. Work marked "later" in these specs (knowledge base, Jonah, drum-buffer-rope enforcement, run transcripts, the AWS deployment with Terraform, KMS and IAM, 1Password) is unscheduled.

(roadmap) Identity and access moved before credentials: phase 3 is identity and access, phase 4 credentials, connectors and the MCP gateway, phase 5 the Goal MCP server. (phase 4) Credentials, connectors and the MCP gateway were 2.2's last phase, F, then phase 3 (the spec was `2.2-f-credentials-and-connectors.md`, then `3-credentials-and-connectors.md`; its parts F1 and F2 became 3.1 and 3.2 and are now 4.1 and 4.2, its PRs `4.1-n`/`4.2-n`). Earlier still they were phases C and G (restructure). Their migrations moved from `00040–00044` to `00050–00054`, after phase 3's. Section references name the part: `4.1 §6.6`, `4.2 §4.3`.

Cross-spec review, build order and open decisions: [review-2.2.md](review-2.2.md). (review)

## Out of 2.2: identity and access (phase 3)

(no-auth) By the user's decision, 2.2 implements no identity, authentication or authorization. This list is the input of [phase 3](3-identity-and-access.md), which settles every item, some differently: Better Auth's tables in schema `auth` instead of Goal's `users`, `memberships`, `invitations` and `sessions`; email codes and Google instead of OIDC; Goal-owned `api_keys` instead of `api_tokens`; ports `Identity`, `Access` and `Platform` instead of `Identity` and `SignIn`, and no `internal/adapters/oidc`; `x-goal-scope` from `api/permissions.yaml` instead of `x-goal-permission`; invitations accepted through Better Auth instead of `/v1/invitations/accept`. Moved to phase 3:
- Tables `users`, `memberships`, `factory_roles`, `invitations`, `sessions`, `api_tokens` (they were A's `00004`, `00005`, `00007`, `00008`; phase 3's migrations are `00040–00049`).
- OIDC sign-in, the session cookie, logout, CSRF and cross-origin protection, API tokens; settings such as `GOAL_AUTH`, `GOAL_OIDC_*`, `GOAL_SESSION_*`, `GOAL_BOOTSTRAP_OWNER_EMAIL`; a non-loopback listen.
- Ports `Identity` and `SignIn`, the package `internal/adapters/oidc` and its depguard rule.
- The permission table (`internal/domain/permission.go`), organization roles (owner, admin, member) and factory roles (editor, approver, viewer), the authorization middleware and role checks, 404 for non-members, the `x-goal-permission` OpenAPI extension and its tests.
- Response fields `role`, `can`, `can_change_role`, `can_remove`, `you`; schemas `Me`, `User`, `UserRef`, `Member`, `Invitation`, `Permission`, `OrgRole`, `FactoryRole`.
- Endpoints `/v1/me`, `/v1/auth/*`, `/v1/invitations/accept`, `/v1/orgs/{org}/members…`, `/invitations…`, `/transfer`; the NOTIFY channel `goal_access` and streams ending on access changes.
- Screens and parts: `Members.dc.html`; the role in Org-Switcher rows, its "Members and roles" link and the account's role and Sign out; Org-Settings' Transfer ownership; D's "Who approves" and human-station approvers (`board.stations[].approvers`); D's read-only mode; role-filtered inbox and attention counts; who may pull or clear the andon.
- The actor taken from a session, and the creator becoming a factory's editor.

**Interim rule: a single local user, every action allowed.** Every request is `domain.LocalUser` (`Actor{user, "local", "Local user"}`), and every action is allowed to it. Organizations separate data (RLS), not people. goald keeps listening on loopback only. Tables that record who did something store `Actor.Ref()` (`user:local`, `agent:<name>`, `system:<job>`) in `<x>_by text` columns, and the audit log stores the actor's kind, id and name; nothing references a `users` table.

**The seam phase 3 builds on** (A §5.3): the request-actor middleware (`internal/api/actor.go`, the only place a request's actor is set) and the `api.Authorizer` interface (`Authorize(ctx, AccessRequest{Actor, Operation, Org, Factory}) error`), wired as `AllowAll` in 2.2 and called from one helper in the `/v1` middleware and in the resolvers `s.factory`/`s.item` (and `s.run`, E13). Specs mark these changes "(no-auth)".

## Shared conventions every spec follows

These keep the specs from colliding. A spec that needs to break one says so in its "Cross-spec contracts" section.

### Decisions already made
- The organization is the tenant boundary. Every org-owned table carries `org_id uuid NOT NULL` referencing `organizations(id)`, with PostgreSQL row-level security: `USING/WITH CHECK (org_id = current_setting('goal.org_id')::uuid)`. goald connects as `goal_app` (no BYPASSRLS); migrations run as `goal_owner`. Each request or engine transaction runs `SET LOCAL goal.org_id`.
- People and access: none in 2.2 (no-auth, above). One local install can hold several organizations, created, renamed and deleted by the local user.
- Credentials belong to an organization and are granted to its factories (phase 4). Working default for phase 4's design pass: PostgreSQL vault with envelope encryption (AES-256-GCM data key per secret version, wrapped by a key-encryption key: local key file, AWS KMS in the cloud). (restructure)
- Definitions: file content in the bucket as immutable blobs `orgs/<org-id>/blobs/<sha256>`; versions in PostgreSQL (`definition_versions`: draft | published, manifest path→sha256, `factories.current_version_id`). Publishing is one transaction; readers are notified with NOTIFY.
- Navigation: organization mark atop a factory rail; factory sidebar Work / Build / Learn; organization pages with an org sidebar. The UI canvas is the visual source of truth.
- Nothing about a factory is code or an environment variable. Environment variables configure only the platform.
- **Interim rule until phase 4 (restructure).** Model access and repository access stay as they are today: platform-level secrets of the local install, resolved through the existing `ports.Secrets` env adapter (`envsecrets`, unchanged signature `Resolve(ctx, ref)`). Model access is goald's `GOAL_TASK_ENV` (default `CLAUDE_CODE_OAUTH_TOKEN=env:CLAUDE_CODE_OAUTH_TOKEN`); the repository token and GitHub MCP headers are `env:GOAL_GITHUB_TOKEN` references in the factory definition. This is a temporary exception to the rule above, removed in phase 4, which introduces `credential:` references and moves these values into the vault. Until then every factory of the install shares these secrets, factory definitions keep their `env:` references (`spec.runtime.repository.token`, connection `headers`, `env`, command words), and 2.2 adds no credential fields, grants or screens. What B validates before phase 4 (B §4.7): each `env:NAME` reference parses and names a variable that is set and non-empty in goald's environment (checked through `ports.Secrets`, value discarded; `secret-env-unset`), and is not reserved (`env-reserved`): one of goald's own settings such as `GOAL_DATABASE_URL` or `GOAL_GATEWAY_KEY`, a platform secret that never reaches a task (`ANTHROPIC_API_KEY`) or a variable `GOAL_TASK_ENV` hands every task (`CLAUDE_CODE_OAUTH_TOKEN`); `env:GOAL_GITHUB_TOKEN` serves only `spec.runtime.repository.token` and the headers of an https connection to GitHub's MCP server (`api.githubcopilot.com`), anywhere else it is `env-reserved` too; a `credential:` reference is an error (`credentials-unavailable`, "credentials arrive with phase 4"); literals keep B's `literal-secret` warning. No grant, expiry or purpose checks: there is nothing to grant before phase 4. Phase 4.1 plugs the vault into B's `authoring.Credentials` extension point. Every organization on the install can reference these variables, so an install with several organizations holds only secrets every one of them may use.

### Naming registry (use these names; add, don't rename)
- API base: `/v1/orgs/{org}/...` (org = slug or id). Factory routes: `/v1/orgs/{org}/factories/{factory}/...`. Items: `/v1/orgs/{org}/items/{item}` (item = UUID or `<factory>#<number>`). The existing unscoped `/v1/...` routes move under the org prefix; the CLI follows. Outside the org prefix in 2.2: `/v1/health`, `/v1/orgs`. Phase 3 adds `/v1/me`, `/v1/auth/config`, `/v1/auth/device/{user_code}`, `/v1/platform/*` and goald's proxy `/api/auth/*` to the auth service (no `/v1/invitations/accept`: invitations are accepted through Better Auth). Phase 4 adds `/v1/oauth/callback` and `/oauth/client-metadata.json`. Phase 5 reserves `/v1/mcp` (the agents' gateway keeps `/mcp`). (review, roadmap)
- Ports (internal/ports): existing `Store, Runner, Task, Workspaces, Checker, Sidecars, Secrets, Contracts, ObjectStore, ItemNotices, CodeHost, Clock`. New in 2.2: `Tenancy`, `Audit` (A); `DefinitionStore`, `DefinitionNotices` (B); `SkillSource` (D); `FlowStats`, optional `Task` interfaces `TranscriptSource` (later) and `TaskInput` (E). Phase 4: `Vault`, `KeyWrapper`, `ModelAuth`, `CredentialProbe` (4.1); `ConnectorCatalog`, `OAuth`, `ToolApprovals`, `Minter`, `MCPRegistry` (4.2). Phase 3: `Identity`, `Access`, `Platform` (`SignIn` dropped: sign-in lives in the `auth/` service), and in `auth/` (TypeScript) the `Mailer` port with its SMTP adapter. In `internal/api`: the `Authorizer` seam (A). Until phase 4, `Secrets` keeps today's signature (restructure). Every port method that touches org-owned rows reads the org from `ctx` (`domain.OrgFrom`, A); an explicit `org` parameter shown in a spec is dropped when the port is written. (review)
- Migration ranges (forward-only, goose): A `00004–00009` (`00004–00007` used: A1's goal_app grants, organizations, org scope, slots; `00008–00009` unused, no-auth: they were sessions and API tokens), B `00010–00019` (`00010–00011` used; `00012` = drop `factories.definition*`, moved out of 2.2: it would run before E's `00030` backfill reads that column; `00013` reserved), E `00030–00039` (`00030–00033` used, `00034` reserved for transcripts, deferred, `00035` the running-runs index of the lost-run sweep), phase 3 `00040–00049` (`00040–00046` planned: auth schema, Better Auth's tables, org lifecycle, identity tables, auth service functions, principal functions, API keys; `00047–00049` reserved), phase 4 `00050–00059` (4.1 vault `00050–00051`, 4.2 connectors `00052–00054`), phase 5 from `00060`. C and D add no tables. `00020–00029` is unused. Later work (2.3's drop of `factories.definition*`) takes the next free number when it is scheduled. (review, restructure, roadmap)
- OpenAPI: one tag per phase: 2.2 `orgs`, `definitions`, `shell`, `builder`, `work`; phase 3 `identity`; phase 4 `credentials` (4.1) and `connectors` (4.2). Schemas in PascalCase, shared types defined once: `Org`, `Factory` (A; B adds `current`, `draft`, `paused`), `Item`, `ItemCard` (E adds flow fields, phase 4 adds `pending_approval`), `Actor` (A; authors and deciders, no-auth: replaces `UserRef`), `Problem` (A adds `code`, B adds `problems[]` of `DefinitionProblem`), `AndonPull` (C defines, E extends), `InboxEntry` (C defines kind `decision`, E adds kinds, phase 4.2 adds `tool_approval` with its `ToolApproval`). Problem `code` values are kebab-case (`draft-conflict`, `credential-name-taken`); a spec showing snake_case codes means the kebab-case spelling. No `x-goal-permission` in 2.2 (no-auth); phase 3 puts `x-goal-scope: <permission>` from `api/permissions.yaml` on every operation (from PR 3-1 on, 2.2 operations added later carry one too) and adds `role` and `can` to `Org`, `OrgSummary` and `Factory`. (review, roadmap)
- Web routes: `/o/:org` (factories home), `/o/:org/f/:factory` (Floor), `/board`, `/items`, `/items/:ref`, `/definition/:section` (D; sections `basics|line|agents|connections|review|versions`), `/agents/:agent` (E owns route and frame; D adds cards, phase 4 a Connections card); org pages `/o/:org/settings`; wizard `/o/:org/new/:step`. Phase 4: `/items/:ref/approvals/:approval`, `/connections`, `/o/:org/catalog`, `/catalog/new`, `/catalog/:connector`, `/credentials`, `/credentials/new`. Phase 3: `/sign-in`, `/sign-in/code`, `/invite/:invitation`, `/device`, `/o/:org/members`, `/o/:org/api-keys` (no-auth, roadmap). Org page route files live under C's pathless `o.$org._org` layout. (review)
- Query keys: root `['org', <org id>, …]` (C). Work keys under `['org', id, 'factory', f, <board|metrics|items|weekly|lead-time|constraint|andon|agent>]` (A, E); other roots under the org: `definition` keys stay at `['org', id, 'factory', f, 'draft'|'versions'|'compare'|…]` (B), `builder` (D), `inbox`, `attention`, `overview`, `settings` (C/E); phase 3: `members`, `invitations`, `api-keys`, and the roots `['auth', 'config']` and `['me']`; phase 4: `credentials`, `connectors`, `approvals`. Stream invalidation names these children explicitly; it never invalidates the bare `['org', id, 'factory', f]` prefix on an item event. (review)
- Events on `/v1/orgs/{org}/stream` (C owns the event union and the client registry; A owns the server feed): `item`, `resync` (existing), `definition` (B), `factory` (E); phase 4 adds `credential`, `approval`, `connection`. NOTIFY channels, every payload with `org_id`: `goal_events` (item events), `goal_definitions` (B), `goal_org_events` (A; carries E's `factory` notices (E9) and phase 4's `credential` and `connection` events), phase 4.2's `goal_approvals`. Phase 3's `goal_access` (session, member, factory role, key and org changes; it carries `org_id` when there is one) only ends streams. (review, roadmap)
- Task token claims (`domain.TaskClaims`): existing `f i s a r c e`; `o` org (A), `v` definition version (B), `aud` = `goal-gateway` (phase 3). Phase 4.2's `g` owner-instance claim is deferred with multi-instance forwarding. (review)
- Bucket: `orgs/<org-id>/blobs/<sha256>` (B), `catalog/templates/` (B), `catalog/skills/` (D), `catalog/connectors/` (phase 4.2); all are definition content (ADR 0005 amendment lists all three catalogs). Nothing else goes in the bucket (AGENTS.md rule 3), so run transcripts are not stored there in 2.2. (review)
- Actors: `domain.Actor` (A); a column recording who did something is `<x>_by text` holding `Actor.Ref()`. No permission table in 2.2: every operation passes A's `Authorizer` seam (no-auth). Phase 3 adds the kind `service` (`service:<key id>`), real users (`user:<uuid>`) and `domain.Principal`; `user:local` rows stay and read "Local user (before sign-in)".
- Audit actions match `^[a-z_]+\.[a-z_]+$`, noun then past-tense verb (`andon.pulled`, `org.updated`). (review)

### Spec template (every spec has these sections, in this order)
1. Summary — what ships, in three sentences.
2. Scope — in / out, explicitly.
3. Canvas boards covered — exact board names from `project/canvas.json`, and any board element this phase does not deliver.
4. Domain and engine changes.
5. Ports and adapters — new or changed interfaces with Go signatures; which package implements each; depguard rules to add.
6. Data model — migrations (tables, columns, types, constraints, indexes, RLS policy, backfill), sqlc queries, conformance-suite additions.
7. Bucket layout changes (if any).
8. API — every endpoint: method, path, request/response schemas, errors (RFC 9457), pagination; SSE events. (no-auth: no required role in 2.2.)
9. CLI changes.
10. Web UI — routes, components, data fetching (TanStack Query keys, SSE invalidation), states (loading, empty, error), accessibility, phone layout; mapped to canvas boards.
11. Security and tenancy — how isolation is enforced (no-auth: roles are phase 3); secrets handling; audit entries written.
12. Observability — logs, metrics, events.
13. Testing — unit tests with port fakes, conformance suite, integration tests (PostgreSQL 18, RustFS), Playwright flows; no useless tests.
14. Rollout and migration — from today's state (single tenant, env secrets) to the new one without losing data.
15. Work breakdown — ordered, small pull requests, each with its acceptance criteria, how it is verified, and the AGENTS.md skills for its areas.
16. Cross-spec contracts — what this phase provides to and needs from other phases (by interface name, endpoint or table).
17. Open questions.

### New packages and their depguard rules (review)
| Package | Phase | May import | Rule |
| --- | --- | --- | --- |
| `internal/retention` | A (A6) | `domain`, `ports`, `factorystore` | `retention` rule: no adapters, engine, `api`, AWS or pgx |
| `internal/factorymodel`, `internal/authoring` | B | `domain`, `ports`, `config`, yaml | `definitions` rule + `authoring` rule (B §5.4) |
| `internal/builder` | D | `domain`, `ports`, `config` | `builder` rule (D §5.2) |
| `internal/adapters/skillssh` | D | own deps | adapters rule |
| `cmd/permgen` (generator: `api/permissions.yaml` → Go and TypeScript) | 3 | standard library, yaml | `cmd` (wiring only) |
| `auth/` (pnpm workspace app, TypeScript) | 3 | `better-auth`, Hono, pg, nodemailer | Biome; `auth/src/mail` imported only by `auth/src` |
| `internal/secrets` | 4.1 | `domain`, `ports` | `secrets` rule (4.1 §5.8) |
| `internal/adapters/keyfile`, `anthropic` | 4.1 | own deps | adapters rule |
| `internal/connectors` | 4.2 | `domain`, `ports`, `config`, `authoring` (to implement `authoring.Check`), `schema`, yaml, jsonschema | `connectors` rule (4.2 §5.5) |
| `internal/adapters/oauthclient`, `awssts`, `mcpregistry` | 4.2 | own deps | adapters rule |

A adds one package, `internal/retention` (A6: the purge job of deleted organizations; no-auth). Phase 3 adds no Go package under `internal/` (its code lives in `internal/api` and `internal/adapters/postgres`) and no `internal/adapters/oidc`. The `api` rule also denies `factorymodel` and `builder` in 2.2 (not `authoring`, whose request and result types `api.Definitions` names, B PR 9), and `secrets` and `connectors` when phase 4 adds them (handlers reach them through interfaces declared in `internal/api`). The `engine` rule should become an allow-list (`domain`, `ports` only), since a deny-list misses the new core packages. Each new row goes into `docs/engineering/architecture.md`'s import table in the PR that adds the package.

### Rules (binding, from AGENTS.md)
Config-driven; factories keyed by UUID; PostgreSQL is the only database; stateless goald; hexagonal architecture enforced by depguard; manual constructor injection in `cmd/*/main.go`; 12-factor config; Terraform only in `terraform/` and not in this phase; no employer names (use `acme`, `example.com`); UI follows `design-system/goal/MASTER.md`; tests protect behaviour; forward-only migrations; one work item, one small pull request; load the AGENTS.md skills for the area a PR touches.
