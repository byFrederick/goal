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
| 3 | [Credentials, connectors and MCP gateway](3-credentials-and-connectors.md) (part 3.1 vault and credentials, part 3.2 connectors and MCP gateway) | After 2.2. A design pass comes first and may rewrite the spec. Works with 2.2's single local user. |
| 4 | Identity and access (no spec yet) | Starts by evaluating options such as Better Auth. Takes everything in "Out of 2.2" below. |

The roadmap stops at phase 4. Work marked "later" in these specs (knowledge base, Jonah, drum-buffer-rope enforcement, run transcripts, the AWS deployment with Terraform, KMS and IAM, 1Password) is unscheduled.

(phase 3) Credentials, connectors and the MCP gateway were 2.2's last phase, F (the spec was `2.2-f-credentials-and-connectors.md`; its parts F1 and F2 are now 3.1 and 3.2, its PRs `F1-n`/`F2-n` are `3.1-n`/`3.2-n`). Earlier still they were phases C and G (restructure). Their migrations keep `00040–00044`, after 2.2's. Section references name the part: `3.1 §6.6`, `3.2 §4.3`.

Cross-spec review, build order and open decisions: [review-2.2.md](review-2.2.md). (review)

## Out of 2.2: identity and access (phase 4)

(no-auth) By the user's decision, 2.2 implements no identity, authentication or authorization; phase 4 looks at the options first. Moved to phase 4:
- Tables `users`, `memberships`, `factory_roles`, `invitations`, `sessions`, `api_tokens` (they were A's `00004`, `00005`, `00007`, `00008`; phase 4's migrations take numbers from `00050` up).
- OIDC sign-in, the session cookie, logout, CSRF and cross-origin protection, API tokens; settings such as `GOAL_AUTH`, `GOAL_OIDC_*`, `GOAL_SESSION_*`, `GOAL_BOOTSTRAP_OWNER_EMAIL`; a non-loopback listen.
- Ports `Identity` and `SignIn`, the package `internal/adapters/oidc` and its depguard rule.
- The permission table (`internal/domain/permission.go`), organization roles (owner, admin, member) and factory roles (editor, approver, viewer), the authorization middleware and role checks, 404 for non-members, the `x-goal-permission` OpenAPI extension and its tests.
- Response fields `role`, `can`, `can_change_role`, `can_remove`, `you`; schemas `Me`, `User`, `UserRef`, `Member`, `Invitation`, `Permission`, `OrgRole`, `FactoryRole`.
- Endpoints `/v1/me`, `/v1/auth/*`, `/v1/invitations/accept`, `/v1/orgs/{org}/members…`, `/invitations…`, `/transfer`; the NOTIFY channel `goal_access` and streams ending on access changes.
- Screens and parts: `Members.dc.html`; the role in Org-Switcher rows, its "Members and roles" link and the account's role and Sign out; Org-Settings' Transfer ownership; D's "Who approves" and human-station approvers (`board.stations[].approvers`); D's read-only mode; role-filtered inbox and attention counts; who may pull or clear the andon.
- The actor taken from a session, and the creator becoming a factory's editor.

**Interim rule: a single local user, every action allowed.** Every request is `domain.LocalUser` (`Actor{user, "local", "Local user"}`), and every action is allowed to it. Organizations separate data (RLS), not people. goald keeps listening on loopback only. Tables that record who did something store `Actor.Ref()` (`user:local`, `agent:<name>`, `system:<job>`) in `<x>_by text` columns, and the audit log stores the actor's kind, id and name; nothing references a `users` table.

**The seam phase 4 builds on** (A §5.3): the request-actor middleware (`internal/api/actor.go`, the only place a request's actor is set) and the `api.Authorizer` interface (`Authorize(ctx, AccessRequest{Actor, Operation, Org, Factory}) error`), wired as `AllowAll` in 2.2 and called from one helper in the `/v1` middleware and in the two resolvers `s.factory`/`s.item`. Specs mark these changes "(no-auth)".

## Shared conventions every spec follows

These keep the specs from colliding. A spec that needs to break one says so in its "Cross-spec contracts" section.

### Decisions already made
- The organization is the tenant boundary. Every org-owned table carries `org_id uuid NOT NULL` referencing `organizations(id)`, with PostgreSQL row-level security: `USING/WITH CHECK (org_id = current_setting('goal.org_id')::uuid)`. goald connects as `goal_app` (no BYPASSRLS); migrations run as `goal_owner`. Each request or engine transaction runs `SET LOCAL goal.org_id`.
- People and access: none in 2.2 (no-auth, above). One local install can hold several organizations, created, renamed and deleted by the local user.
- Credentials belong to an organization and are granted to its factories (phase 3). Working default for phase 3's design pass: PostgreSQL vault with envelope encryption (AES-256-GCM data key per secret version, wrapped by a key-encryption key: local key file, AWS KMS in the cloud). (restructure)
- Definitions: file content in the bucket as immutable blobs `orgs/<org-id>/blobs/<sha256>`; versions in PostgreSQL (`definition_versions`: draft | published, manifest path→sha256, `factories.current_version_id`). Publishing is one transaction; readers are notified with NOTIFY.
- Navigation: organization mark atop a factory rail; factory sidebar Work / Build / Learn; organization pages with an org sidebar. The UI canvas is the visual source of truth.
- Nothing about a factory is code or an environment variable. Environment variables configure only the platform.
- **Interim rule until phase 3 (restructure).** Model access and repository access stay as they are today: platform-level secrets of the local install, resolved through the existing `ports.Secrets` env adapter (`envsecrets`, unchanged signature `Resolve(ctx, ref)`). Model access is goald's `GOAL_TASK_ENV` (default `CLAUDE_CODE_OAUTH_TOKEN=env:CLAUDE_CODE_OAUTH_TOKEN`); the repository token and GitHub MCP headers are `env:GOAL_GITHUB_TOKEN` references in the factory definition. This is a temporary exception to the rule above, removed in phase 3, which introduces `credential:` references and moves these values into the vault. Until then every factory of the install shares these secrets, factory definitions keep their `env:` references (`spec.runtime.repository.token`, connection `headers`, `env`, command words), and 2.2 adds no credential fields, grants or screens. What B validates before phase 3 (B §4.7): each `env:NAME` reference parses and names a variable that is set and non-empty in goald's environment (checked through `ports.Secrets`, value discarded; `secret-env-unset`), and is not one of goald's own settings such as `GOAL_DATABASE_URL` or `GOAL_GATEWAY_KEY` (`env-reserved`); a `credential:` reference is an error (`credentials-unavailable`, "credentials arrive with phase 3"); literals keep B's `literal-secret` warning. No grant, expiry or purpose checks: there is nothing to grant before phase 3. Phase 3.1 plugs the vault into B's `authoring.Credentials` extension point. Every organization on the install can reference these variables, so an install with several organizations holds only secrets every one of them may use.

### Naming registry (use these names; add, don't rename)
- API base: `/v1/orgs/{org}/...` (org = slug or id). Factory routes: `/v1/orgs/{org}/factories/{factory}/...`. Items: `/v1/orgs/{org}/items/{item}` (item = UUID or `<factory>#<number>`). The existing unscoped `/v1/...` routes move under the org prefix; the CLI follows. Outside the org prefix in 2.2: `/v1/health`, `/v1/orgs`. Phase 3 adds `/v1/oauth/callback` and `/oauth/client-metadata.json`; `/v1/me`, `/v1/auth/*` and `/v1/invitations/accept` are phase 4 (no-auth). (review)
- Ports (internal/ports): existing `Store, Runner, Task, Workspaces, Checker, Sidecars, Secrets, Contracts, ObjectStore, ItemNotices, CodeHost, Clock`. New in 2.2: `Tenancy`, `Audit` (A); `DefinitionStore`, `DefinitionNotices` (B); `SkillSource` (D); `FlowStats`, optional `Task` interfaces `TranscriptSource` (later) and `TaskInput` (E). Phase 3: `Vault`, `KeyWrapper`, `ModelAuth`, `CredentialProbe` (3.1); `ConnectorCatalog`, `OAuth`, `ToolApprovals`, `Minter`, `MCPRegistry` (3.2). Phase 4: `Identity`, `SignIn` (moved out of A, no-auth). In `internal/api`: the `Authorizer` seam (A). Until phase 3, `Secrets` keeps today's signature (restructure). Every port method that touches org-owned rows reads the org from `ctx` (`domain.OrgFrom`, A); an explicit `org` parameter shown in a spec is dropped when the port is written. (review)
- Migration ranges (forward-only, goose): A `00004–00009` (`00004–00006` used; `00007–00009` unused, no-auth: they were sessions and API tokens), B `00010–00019` (`00010–00011` used; `00012` = drop `factories.definition*`, moved out of 2.2: it would run before E's `00030` backfill reads that column; `00013` reserved), E `00030–00039` (`00030–00033` used, `00034` reserved for transcripts, deferred), phase 3 `00040–00049` (3.1 vault `00040–00041`, 3.2 connectors `00042–00044`). C and D add no tables. `00020–00029` is unused. `00050+` for anything later: phase 4's identity tables and 2.3's drop of `factories.definition*`. (review, restructure)
- OpenAPI: one tag per phase: 2.2 `orgs`, `definitions`, `shell`, `builder`, `work`; phase 3 `credentials` (3.1) and `connectors` (3.2). Schemas in PascalCase, shared types defined once: `Org`, `Factory` (A; B adds `current`, `draft`, `paused`), `Item`, `ItemCard` (E adds flow fields, phase 3 adds `pending_approval`), `Actor` (A; authors and deciders, no-auth: replaces `UserRef`), `Problem` (A adds `code`, B adds `problems[]` of `DefinitionProblem`), `AndonPull` (C defines, E extends), `InboxEntry` (C defines kind `decision`, E adds kinds, phase 3.2 adds `tool_approval` with its `ToolApproval`). Problem `code` values are kebab-case (`draft-conflict`, `credential-name-taken`); a spec showing snake_case codes means the kebab-case spelling. No `x-goal-permission` in 2.2 (no-auth). (review)
- Web routes: `/o/:org` (factories home), `/o/:org/f/:factory` (Floor), `/board`, `/items`, `/items/:ref`, `/definition/:section` (D; sections `basics|line|agents|connections|review|versions`), `/agents/:agent` (E owns route and frame; D adds cards, phase 3 a Connections card); org pages `/o/:org/settings`; wizard `/o/:org/new/:step`. Phase 3: `/items/:ref/approvals/:approval`, `/connections`, `/o/:org/catalog`, `/catalog/new`, `/catalog/:connector`, `/credentials`, `/credentials/new`. Phase 4: `/o/:org/members`, `/invite/:token` (no-auth). Org page route files live under C's pathless `o.$org._org` layout. (review)
- Query keys: root `['org', <org id>, …]` (C). Work keys under `['org', id, 'factory', f, <board|metrics|items|weekly|lead-time|constraint|andon|agent>]` (A, E); other roots under the org: `definition` keys stay at `['org', id, 'factory', f, 'draft'|'versions'|'compare'|…]` (B), `builder` (D), `inbox`, `attention`, `overview`, `settings` (C/E); phase 3: `credentials`, `connectors`, `approvals`. Stream invalidation names these children explicitly; it never invalidates the bare `['org', id, 'factory', f]` prefix on an item event. (review)
- Events on `/v1/orgs/{org}/stream` (C owns the event union and the client registry; A owns the server feed): `item`, `resync` (existing), `definition` (B), `factory` (E); phase 3 adds `credential`, `approval`, `connection`. NOTIFY channels, every payload with `org_id`: `goal_events` (item events; E's factory notices), `goal_definitions` (B), `goal_org_events` (A; carries phase 3's `credential` and `connection` events), phase 3.2's `goal_approvals`. `goal_access` moved to phase 4 (no-auth). (review)
- Task token claims (`domain.TaskClaims`): existing `f i s a r c e`; `o` org (A), `v` definition version (B). Phase 3.2's `g` owner-instance claim is deferred with multi-instance forwarding. (review)
- Bucket: `orgs/<org-id>/blobs/<sha256>` (B), `catalog/templates/` (B), `catalog/skills/` (D), `catalog/connectors/` (phase 3.2); all are definition content (ADR 0005 amendment lists all three catalogs). Nothing else goes in the bucket (AGENTS.md rule 3), so run transcripts are not stored there in 2.2. (review)
- Actors: `domain.Actor` (A); a column recording who did something is `<x>_by text` holding `Actor.Ref()`. No permission table in 2.2: every operation passes A's `Authorizer` seam (no-auth).
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
11. Security and tenancy — how isolation is enforced (no-auth: roles are phase 4); secrets handling; audit entries written.
12. Observability — logs, metrics, events.
13. Testing — unit tests with port fakes, conformance suite, integration tests (PostgreSQL 18, RustFS), Playwright flows; no useless tests.
14. Rollout and migration — from today's state (single tenant, env secrets) to the new one without losing data.
15. Work breakdown — ordered, small pull requests, each with its acceptance criteria, how it is verified, and the AGENTS.md skills for its areas.
16. Cross-spec contracts — what this phase provides to and needs from other phases (by interface name, endpoint or table).
17. Open questions.

### New packages and their depguard rules (review)
| Package | Phase | May import | Rule |
| --- | --- | --- | --- |
| `internal/factorymodel`, `internal/authoring` | B | `domain`, `ports`, `config`, yaml | `definitions` rule + `authoring` rule (B §5.4) |
| `internal/builder` | D | `domain`, `ports`, `config` | `builder` rule (D §5.2) |
| `internal/adapters/skillssh` | D | own deps | adapters rule |
| `internal/secrets` | 3.1 | `domain`, `ports` | `secrets` rule (3.1 §5.8) |
| `internal/adapters/keyfile`, `anthropic` | 3.1 | own deps | adapters rule |
| `internal/connectors` | 3.2 | `domain`, `ports`, `config`, `authoring` (to implement `authoring.Check`), `schema`, yaml, jsonschema | `connectors` rule (3.2 §5.5) |
| `internal/adapters/oauthclient`, `awssts`, `mcpregistry` | 3.2 | own deps | adapters rule |

A adds no package (no-auth: `internal/adapters/oidc` moved to phase 4). The `api` rule also denies `internal/authoring`, `factorymodel` and `builder` in 2.2, and `secrets` and `connectors` when phase 3 adds them (handlers reach them through interfaces declared in `internal/api`). The `engine` rule should become an allow-list (`domain`, `ports` only), since a deny-list misses the new core packages. Each new row goes into `docs/engineering/architecture.md`'s import table in the PR that adds the package.

### Rules (binding, from AGENTS.md)
Config-driven; factories keyed by UUID; PostgreSQL is the only database; stateless goald; hexagonal architecture enforced by depguard; manual constructor injection in `cmd/*/main.go`; 12-factor config; Terraform only in `terraform/` and not in this phase; no employer names (use `acme`, `example.com`); UI follows `design-system/goal/MASTER.md`; tests protect behaviour; forward-only migrations; one work item, one small pull request; load the AGENTS.md skills for the area a PR touches.
