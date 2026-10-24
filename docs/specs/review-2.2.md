# Phase 2.2 cross-spec review

Scope: the seven specs as they were then (A–G; then A–F; now 2.2 A–E plus phase 4, see the next sections), README conventions, AGENTS.md, architecture, testing, frontend, ADRs 0001–0006, MASTER.md and the 37 canvas boards. Every change made in a spec ends with "(review)".

## Changes after the review

### Roadmap restructure: identity before credentials (roadmap)

The user moved identity and access ahead of credentials and added a last phase. The roadmap is now 2.2 (A–E) → **phase 3, identity and access** ([3-identity-and-access.md](3-identity-and-access.md), [ADR 0007](../adr/0007-identity-better-auth.md)) → **phase 4, credentials, connectors and MCP gateway** (`3-credentials-and-connectors.md` renamed [4-credentials-and-connectors.md](4-credentials-and-connectors.md), parts 4.1 and 4.2, PRs `4.1-n`/`4.2-n`, migrations `00050–00054`) → **phase 5, Goal MCP server** ([5-goal-mcp-server.md](5-goal-mcp-server.md), stub). It stops at phase 5. Phase 3 takes `00040–00049`, phase 4 `00050–00059`, phase 5 `00060` up; none was applied, so renumbering is safe.
- Throughout this file and the 2.2 specs, the old "phase 4" (identity) now reads "phase 3", the old "phase 3", "3.1" and "3.2" (credentials, connectors) read "phase 4", "4.1" and "4.2". Decisions that were left to "phase 3's design pass" are phase 4's; those left to "phase 4" are settled in phase 3's spec.
- The interim env-secrets rule (README) now lasts until phase 4, and 2.2's single local user until phase 3.
- Phase 3 settles the "Out of 2.2" list with Better Auth: sessions checked per request in PostgreSQL, goald-owned API keys and organization lifecycle, `api/permissions.yaml` with `x-goal-scope`; its spec lists where it departs from the list.

Two later user decisions reshaped the plan. Everything below "Restructure after review" is kept as history, with phase names updated; rows about roles, permissions, members, invitations or sign-in are superseded by the first change.

### No authentication or authorization in 2.2 (no-auth)

The user decided that 2.2 implements no identity, authentication or authorization, so that phase 3 can first look at options such as Better Auth. Each edit is marked "(no-auth)".
- **What moved to phase 3, "Identity and access":** the precise list is the README's "Out of 2.2". In short: `users`, `memberships`, `factory_roles`, `invitations`, `sessions`, `api_tokens`; OIDC, sessions, logout, CSRF, API tokens; ports `Identity` and `SignIn` and `internal/adapters/oidc`; the permission table, role checks, 404-for-non-members, `x-goal-permission`, `role`/`can` fields; `/v1/me`, `/v1/auth/*`, members, invitations and transfer endpoints; `goal_access`; Members, Who approves, read-only mode, role-filtered counts, Transfer ownership.
- **Interim rule:** a single local user (`domain.LocalUser`), every action allowed; goald stays on loopback; "who did it" columns hold `Actor.Ref()` text.
- **Seam:** A's request-actor middleware and `api.Authorizer` (`AllowAll` in 2.2), called once in the `/v1` middleware and in the resolvers `s.factory`/`s.item` (A §5.3). Two tests keep every operation on the seam (A §11.3).
- **A is now "Organizations and tenancy":** PRs A1–A7 (database roles with goal_app's grants `00004`, tenancy tables `00005`, org scope `00006`, org-aware engine and bucket, route move and seams, organization management, slot caps `00007`; renumbered by A1). A8a, A8b and A9 moved to phase 3; `00008–00009` stay unused because phase 3 numbers its migrations from `00040` (a lower number merged after B's `00010` would break goose).
- **B:** no role checks; `created_by`, `updated_by`, `published_by`, `paused_by` are actor-ref text; `board.stations[].approvers`, `authoring.Members` and `UserRef` removed (`Actor` instead). **C:** no Members screen or route (C6 moved), the org menu lists every organization with no role and no Members link, the account menu shows the local user, Org settings keep General, slots and Delete, nothing is hidden by role. **D:** no Who approves, human stations are decided by the local user, no read-only mode, no creator-becomes-editor. **E:** move, pause, send back, notes, andon pull and clear are allowed to the local user; "waiting on you" means waiting on a person (every decision); `InboxSource.Inbox(ctx)` keeps the actor in `ctx`. **Phase 4:** no credential or connector roles; its design pass notes that access control arrives with phase 3.

### Credentials, connectors and MCP gateway leave 2.2 (phase 4)

The user then moved credentials, connectors and the MCP gateway out of 2.2 and set the roadmap of the time: credentials, connectors and MCP gateway first, then identity and access, nothing after. The roadmap restructure above swapped the two; this section uses today's numbers (credentials phase 4, identity phase 3).
- 2.2 is A–E only. `2.2-f-credentials-and-connectors.md` became `3-credentials-and-connectors.md` and is now `4-credentials-and-connectors.md`, "Phase 4 — Credentials, connectors and MCP gateway", with part 4.1 (vault and credentials, PRs `4.1-1…4.1-9`) and part 4.2 (connectors and MCP gateway, PRs `4.2-1…4.2-26`). Its migrations were `00040–00044` and are now `00050–00054`, after phase 3's. Its design pass stays first.
- Every "phase F", "F1", "F2" in the 2.2 specs now reads "phase 4", "phase 4.1", "phase 4.2"; the interim env-secrets exception lasts until phase 4; the extension points phase F plugged into (`authoring.Credentials`, `authoring.Check`/`Resolver`, `capabilities.credentials`, C's settings placeholders and sidebar, `InboxSource`, `ItemCard.pending_approval`, E's block reason) are phase 4's.
- Older references of the first roadmap (knowledge base, Terraform and hosted deployment in its phase 3; drum-buffer-rope, automatic andon and the constraint cache in its phase 4) now say "later" (unscheduled), since the numbers have new meanings; the roadmap stops at phase 5. User-facing copy "Enforced from phase 4" (old numbering) became "Not enforced yet".

### Completeness pass (after both changes)

Checked the five 2.2 specs against each other and AGENTS.md. Fixed:
- D's screen-registry flips named D8 and D14; the PR table says D7 (Definition) and D13 (New factory, Import YAML). §10.1 now matches.
- E still listed transcript work as in scope in its adapters table, CLI, route, rollout, B contract and a Playwright flow; each is now marked deferred.
- E's Rail row named `Inbox.factories[]`, which the review removed; it now names C's `/attention`.
- C's rollout said A seeds `acme`; A seeds `default` (renamed by the user).
- A's schema test allowlist no longer names phase 4 tables; phase 4 extends it in its own PRs, so A–E don't depend on phase 4.
- Open questions already answered here are marked so in C §17 and D §17.
- Each spec's §15 now names the AGENTS.md skills for its PR areas (README template item 15).
- B's CLI org resolution points at A §9 (A no longer has a local "default org" concept beyond the migrated `default`).

Not fixed here (need a decision or are outside `docs/specs/`):
- The repository README, AGENTS.md (rule 3 "from phase 4, knowledge documents"), `docs/engineering/architecture.md` and ADRs still describe the old roadmap (knowledge base and Terraform in phase 4). They should be updated in their own PR.
- The literal SQL of later migrations is described by kit notes, not rewritten (unchanged from "Not resolved here").
- Cross-origin protection for the loopback API moved to phase 3 with CSRF; decision 12 asks whether to keep it in A5 instead.

## Restructure after review (history)

(restructure) After this review the user decided that credentials and the vault belong with connectors, and that the whole area should move to the end so it can be designed carefully. The findings below are kept as history, with phase letters, PR ids, sections and migration numbers updated to the current plan (2.2 A–E, then phase 4).

| Now | Spec | Was in the review (A–G) |
| --- | --- | --- |
| A | Organizations and tenancy (`2.2-a-organizations.md`; "Organizations and identity" until no-auth) | A |
| B | Definitions and versions (`2.2-b-definitions.md`) | B |
| C | App shell and organization screens (`2.2-c-app-shell.md`) | D |
| D | Factory builder (`2.2-d-factory-builder.md`) | E |
| E | Work screens and flow metrics (`2.2-e-work-screens.md`) | F |
| Phase 4 | Credentials, connectors and MCP gateway (`4-credentials-and-connectors.md`): part 4.1 vault and credentials, part 4.2 connectors and MCP gateway | C + G (then 2.2's F) |

- **One phase for credentials and connectors.** Phase 4 merges the old vault spec (4.1, PRs `4.1-1…4.1-9`, migrations now `00050–00051`, earlier `00040–00041` and first `00020–00021`) and the old connectors spec (4.2, PRs `4.2-1…4.2-26`, migrations now `00052–00054`, earlier `00042–00044`). It opens with a design pass (threat model, credential lifecycle, OAuth and MCP authorization, secret storage options, per-run minting, approval-gated tools, connector packaging, and now access control for phase 3) that may rewrite it. OpenAPI tags: `credentials` (was `vault`) and `connectors`.
- **Interim rule until phase 4 (README).** Model access (`GOAL_TASK_ENV` → `CLAUDE_CODE_OAUTH_TOKEN`) and repository access (`env:GOAL_GITHUB_TOKEN`) stay platform secrets of the install, resolved through today's `ports.Secrets` env adapter, as a marked, temporary exception to "nothing about a factory is an environment variable". Definitions keep their `env:` references. B validates only that each `env:` reference names a set variable that is not one of goald's own settings, and refuses `credential:`; no grant checks.
- **Stripped from A–E** (each edit marked "(restructure)"): A's settings carry no model-access or vault fields; B's `credentials` check runs on the interim `EnvReferences` behind `authoring.Credentials` (no vault types), and `FactoryModel` loses the credential fields until phase 4.1; C's Org settings show Model access and Vault as "Arrives with credentials", `OrgAttention` loses `credentials_needing_attention`, the Credentials entry has no dot, C7 no longer needs vault kinds; D's Basics shows how the repository and model access sign in read-only (new `capabilities.credentials` flag), D6 no longer waits for `CredentialSelect`, the mock has no credentials; E shows block reasons as text, and `credential_unavailable` arrives with phase 4.1.
- **What moved under phase 4's design pass:** decision 4 (credential rotation), phase 4.1's open questions, and the review of held tool approvals (decision 10 keeps its default for now).
- **Why it fits.** No phase before phase 4 imports a vault type or calls a vault endpoint; every A–E pull request can merge without phase 4; Phase 4 plugs into named extension points (`authoring.Credentials`, `capabilities.credentials`, C's settings placeholders and sidebar, E's block reason).

## Verdict

**Buildable after the fixes below.** The seven specs (five 2.2 specs plus phase 4 since the later changes) now agree on names, paths, tables, events and ports, and the build order below has no cycles. The plan respects the architecture rules once the scope cuts are applied: drum-buffer-rope, the automatic andon pull and the constraint cache move to later (unscheduled); run transcripts move out (bucket rule); multi-instance gateway forwarding moves to the AWS deployment.

Two things a builder must keep in mind:
1. **Migration PRs merge in number order.** goose (`NewProvider`, default settings) refuses a lower-numbered migration that arrives after a higher one is applied. Each phase's other PRs can run in parallel.
2. **Eight product decisions** remain open for 2.2 (last section: 2, 3, 5, 6, 7, 8, 11 and the new 12). Decisions 4 and 10 moved to phase 4's design pass, decision 1 to phase 3, and decision 9 is moot in 2.2 (no-auth). Each open one has a recommended default, so building can start on those defaults.

Fixes: 48 rows in the table below, made as 182 marked edits across the README and the seven specs. By type: contracts 22, collisions 9, architecture 9, scope 6, build order 2.

## Fixes applied

| Spec | Section | Change | Why |
| --- | --- | --- | --- |
| README | Registry | Registered every new port, package, route, query-key root, stream event, NOTIFY channel, token claim, bucket prefix, migration number, shared schema; added the package/depguard table | Rule 2: one registry; specs had introduced ~40 unregistered names |
| README | Registry | Problem `code` is kebab-case; audit actions `noun.past_verb`; org from `ctx`; permissions only in A's table | B and D used kebab-case, phase 4.1 and phase 4.2 snake_case; E's audit names failed A's CHECK regex; Phase 4.1 and phase 4.2 passed `org` explicitly |
| A | §5.1, §8.5, §16 | New `goal_org_events` channel and `ItemNotice.Org` for non-item stream events; the feed filters every event by org and visible factory | Phase 4.1's `credential` event relied on an "org-event NOTIFY" A did not have; Phase 4.2 invented `goal_connections` |
| A | §5.1 | `api.Factories` listing moves to B's `api.Definitions` | A said "B keeps this", B removed it |
| A | §3, §8.2, §16 | `GET /settings` needs `org.read`; model access and vault live on phase 4.1's `/settings/vault`; C's Save sends both requests | C shows Settings read-only to members; D needs slot caps; A and phase 4.1 disagreed on one vs two endpoints |
| A | §8.7 | Added `UserRef`, `Problem.code` | B needed `UserRef`; B, phase 4.1, D, phase 4.2 need `code` |
| A | §10 | C's `subscribe(baseUrl, {org, factory?}, handlers)`; keys use the org id | A and C had different signatures; C keys by id |
| A | §11.2 | `credentials.read` → admin; added `andon.pull` (viewer), `item.move/pause/note`, `andon.clear` (approver), `factory.pause` (editor); `item.retry` → approver; `connections.bind` → `connections.test`; `factory.delete` marked later | Phase 4.1's list is admin-only; E's operator actions had no rows; Pause/Resume need the same role; Phase 4.2 binds through definition edits |
| A | §6.1, §6.6 | `goal_org()` stays strict, phase 4.2 adds `goal_org_or_null()`; RLS test allowlist gains `vault_keys`; `connectors` named policies | Phase 4.2 had redefined `goal_org()`; Phase 4.1's platform table and phase 4.2's curated rows would fail the guard test |
| A | §15 | A8 split: A8a sessions migration 00007 right after A7; A8b OIDC (no migration) any time later | 00007 landing after B's 00010 breaks goose |
| B | §2, §17 | Weekly budget enforcement is D's | D took it (B Q1) |
| B | §4.7, §5.3, §16 | `authoring.Credentials` = phase 4.1's `Vault.Check`; `authoring.Connectors`/`ConnectorInfo` dropped; Phase 4.2's checker is an `authoring.Check` + `authoring.Resolver`; `Members` = A's `Tenancy.Members` | B needed `CredentialStates` and `Connectors(refs)`, which phase 4.1 and phase 4.2 don't provide; A has no `Approvers` |
| B | §5.1, §6.1 | `Publish.Resolved`, `Publish.Bindings`; `definition_versions.resolved jsonb` | Resolves B Q5 and phase 4.2 Q5: runs on older versions read their pinned bindings; B's tx projects `connections` |
| B | §6.1 | A's tenancy kit (`DEFAULT goal_org()`, `ON DELETE CASCADE`, `org_isolation` with `(SELECT goal_org())`) | Rule: RLS + org_id per A; purge needs cascade |
| B | §6.1, §14, §15 | `00012` (drop `factories.definition`) moved to 2.3; `00011` moved into PR 3 | E's 00030 backfill reads that column and runs after 00012 on a fresh DB; migration order |
| B | §7, §14 | Legacy import reads A's interim `orgs/<org>/factories/<id>/`, not `factories/<id>/` | A4 moves and deletes the old keys before B runs |
| B | §8.1, §8.3, §16 | `GET /factories` is one operation returning A's `Factory` + `current/draft/paused`; `FactorySummary` gone; roles mapped to permissions | Path defined twice (A, B); `FactorySummary` collided with C's schema |
| B | §8.2 | `Validation.problems` typed `DefinitionProblem`; FactoryModel gains `repository.credential`, `model_credential`, `skills[].origin`, `flow.targets` | D §16 asked for them (phase 4.1's, D's, E's fields) |
| B | §8.4, §16, §18 | Stream event union owned by C, feed by A; ADR amendment lists all three catalogs | Owner was "E/C"; rule 3 needed the catalogs named |
| Phase 4.1 | §5.2 | Org from `ctx`, explicit `org` params dropped | README convention (A) |
| Phase 4.1 | §6.1, §6.2 | Tenancy kit, `org_isolation` names, cascade, composite `credential_uses.run_id` | A's guard test requires `org_isolation`; purge |
| Phase 4.1 | §6.6 | `credentials.connector`/`method` text (not `connector_id`), `needs_reconnect`, sealing inside the postgres adapter, `Resolve`/`Rotate` mapping for phase 4.2 | Phase 4.2's needs (`AddVersion`, `SetStatus`, `MarkUsed`, `Seal/Open`) mapped onto phase 4.1's port without new core methods except `MarkNeedsReconnect` |
| Phase 4.1 | §8 | Roles → permissions; kebab codes; `GET /settings/vault` is `org.read`; SSE via `goal_org_events` | C's read-only settings; channel gap |
| Phase 4.1 | §10.1 | Route files under `o.$org._org` | C's org-sidebar layout |
| C | §2, §8, §10, §15, §16 | `/factories/summary` → `/overview` (`FactoryOverview[List]`); `/waiting` → `/inbox` shared with E (`InboxSource` registry in C2) | `summary` shadowed a factory named "summary"; C and E each built an inbox |
| C | §4, §5 | One `domain.AndonPull` (E extends); `Store.WaitingHuman` interim until E's `InboxItems` | Duplicate type; same postgres type implements both ports |
| C | §8 | `budget_usd` from `weeklyUsd` | B/D schema (C Q3) |
| C | §10.8, §10.9, §16 | Members and invitations separate queries; settings PATCH on `/settings` + phase 4.1's vault endpoint; slots from A7 | A's actual endpoints |
| C | §10.10 | `invalidationsFor` names work keys one by one; never the bare factory prefix | Item bursts would refetch B's drafts and phase 4.1's factory credentials (D's finding) |
| C | §10.1, §16 | `/agents/:agent` owned by E; Credentials pages by phase 4.1 | Two specs claimed the route; "phase 4.1 or phase 4.2" |
| C | §10.11 | New tokens go into MASTER.md first | Rule 10; tokens are generated from MASTER.md |
| D | §10.6 | DBR queue sentence and Flow panel say "not enforced yet" (was "enforced from phase 3" under the old roadmap) | Scope cut in E |
| D | §5.2 | `api` rule denies `internal/builder` | Boundary listed for every new package |
| D | §16 | Query-key issue resolved; slots via `GET /settings`; `builtins: ask` stays a warning | Fixed in C/E; A change; Phase 4.2 gates gateway tools only |
| E | §1, §2 | Deferred list: DBR release, expedite limit, automatic rework pull, constraint cache, transcripts | Unscheduled flow-control work (was the old roadmap's phase 3); rule 3 |
| E | §4.3, §4.4, §4.7, §7 | Marked deferred; manual andon still stops the line | Canvas needs "Pull the andon cord" to act |
| E | §4.5 | `by domain.Actor` on every engine method | A §4.2 |
| E | §5.1, §5.2 | `FlowStats.Attention` → `InboxItems`; `ReleaseUpTo`, `SetRunTranscript` deferred | Method-name clash with C's `Store.Attention` on one Go type |
| E | §6 | Tenancy kit, composite parent keys; backfill tolerates NULL `factories.definition` | Rule; B stops writing that column at B7 |
| E | §8 | Roles → permissions; `Board.line.release.enforced`, `budget`; `ItemCard.pending_approval`; `/inbox` is C's without `factories[]`; Phase 4.2's `ToolApproval`; no `by` | Contracts with A, C, D, phase 4.2 |
| E | §10.3, §11 | Explicit invalidation keys; audit names `andon.pulled`, … ; no transcript read audit | Collision; A's CHECK regex |
| E | §13, §15 | E8 display only, E9 manual andon, E11 extends C's inbox, E13 runs without transcripts, E14 split a/b; deferred tests noted | Scope |
| Phase 4.2 | §1, §2, §4.5, §4.7, §5.3, §14, §15 | Forwarding (`g` claim, `GOAL_PEER_URL`, 4.2-16) deferred to the AWS deployment | One goald in 2.2; no board needs it |
| Phase 4.2 | §4.3 | Checker = `authoring.Check` + `Resolver`; kebab codes; three codes merged with B's; `blocked` severity → warning | B's pipeline shape and severities |
| Phase 4.2 | §5.1, §5.5 | Org from `ctx`; sealing in the adapter; `connectors` may import `authoring`; `api` denies `connectors` | Conventions, depguard |
| Phase 4.2 | §6 | `goal_org_or_null()` only for `connectors`; tenancy kit elsewhere; adds phase 4.1's credential columns; `connection` event on `goal_org_events` | A's strict helper; channel registry |
| Phase 4.2 | §8, §10 | Roles → permissions; query keys under `['org', id]` | A's table; C's root |
| Phase 4.2 | §16 | Needs from A, B, phase 4.1, C/E rewritten to what those specs provide; no `JobLocks` port | Contract mismatches |

## Contract matrix

| Provider → consumer | Interface | Status |
| --- | --- | --- |
| A → all | `domain.OrgID/Actor` (`LocalUser`, `Ref`), `WithOrg/OrgFrom`, `WithActor/ActorFrom`, `ErrNoOrg`, tenancy SQL kit, `goal_owner`/`goal_app` | ok (no-auth: `UserID` removed) |
| A → all | `Authorizer` seam (`AllowAll`) and `s.factory/s.item` resolvers | no-auth: the permission table and `x-goal-permission` moved to phase 3 |
| A → B, phase 4.1, E, phase 4.2 | `Tenancy.ActiveOrgs` for cross-org jobs (refresh, rewrap, sweeps) | ok (answers phase 4.1 and phase 4.2 needs) |
| A → B | (station approvers) | no-auth: removed; phase 3 |
| A → C | `/orgs`, `/orgs/{org}`, `/settings`, DELETE org | fixed (no-auth: `/me`, `/members`, `/invitations`, `/transfer` moved to phase 3) |
| A → D | `GET /settings` slots | fixed |
| A ↔ C | `subscribe(baseUrl, {org, factory?}, handlers)` | fixed |
| A → all | `Problem.code` (kebab-case), `Actor` | fixed (no-auth: `UserRef` replaced by `Actor`) |
| A → phase 4.1, phase 4.2 | `goal_org_events` NOTIFY, `ItemNotice.Org` | fixed (new) |
| A → phase 4.2 | `goal_org()` strict; Phase 4.2's own `goal_org_or_null()` | fixed |
| A, B → phase 4.2 | Task token claims `o`, `v` | ok (`g` deferred) |
| B → A, C, D | `GET /factories` → `Factory` (+`current`, `draft`, `paused`) | fixed (collision) |
| B → D | Draft/model/files/validate/publish/versions/compare/templates API, `defKeys`, `FactoryModel` | fixed (model additions) |
| B → C, D, E | `definition` stream event from `goal_definitions` | fixed (owner) |
| B → E | `agent_runs.definition_version_id`, `moved` with `reason`, `paused` | ok |
| B → phase 4.2 | `definition_versions.resolved`, `Publish.Bindings` → `connections` rows in the publish tx | fixed (new) |
| B → phase 4.1 | Draft/publish to rewrite references at cutover | ok (4.1-9 comes long after B10) |
| B → phase 4.1 | `authoring.Credentials` extension point, `config.Factory.SecretUses()`; interim `EnvReferences` until phase 4 | restructure (no vault types in B) |
| Phase 4.1 → B | `authoring.VaultCredentials` over `Vault.Check(ctx, factory, uses)` | fixed (was `CredentialStates`); arrives with phase 4 (restructure) |
| Phase 4.1 → C | `/settings/vault` replacing C's placeholders; `credentials_needing_attention` added to `/attention` from `Vault.List` statuses (admins; 0 for others) | fixed; arrives with phase 4 (restructure) |
| Phase 4.1 → D | `CredentialSelect`, `GET /factories/{f}/credentials`, `credential` event, `capabilities.credentials` | arrives with phase 4; D ships read-only lines (restructure) |
| Phase 4.1 → E | Block `reason_code: credential_unavailable` linked to the credential | arrives with phase 4 (restructure) |
| Phase 4.1 → phase 4.2 | `Vault.Resolve`/`Rotate`/`Credential`, `NewCredential.Connector/Method`, `MarkNeedsReconnect`, `envelope.go` in-adapter | fixed (mapped) |
| C → B, phase 4.1, D, E, phase 4.2 | Route tree, `screens.ts`, `streamHandlers`, `invalidationsFor`, UI inventory | fixed (explicit keys) |
| C → E, phase 4.2 | `/attention`, `/inbox` + `InboxSource` registry, `domain.AndonPull` | fixed (merged with E's inbox) |
| C → B, E, phase 4.2 | `/overview` (`FactoryOverview`) | fixed (renamed) |
| D → B, E, C | Weekly budget enforcement, `Store.SpendSince`, `Board.line.budget` | ok (E's Board field added) |
| D → E, phase 4.2 | Agent page cards; `shared/connections-editor` slot; `/definition/*` deep links | ok |
| E → C | Inbox kinds, `FlowStats.InboxItems`, extended `AndonPull`, `event: factory` | fixed |
| E → D | `flow.targets`, Agent page frame and route | ok (route ownership fixed) |
| E → phase 4.2 | `ItemCard.pending_approval`, item timeline | fixed (accepted) |
| Phase 4.2 → B | `connectors.Checker` as `authoring.Check`/`Resolver`, problem codes | fixed |
| Phase 4.2 → C, E | `tool_approval` `InboxSource`, `/approvals`, `approval`/`connection` events, `connections_blocked` | fixed |
| Phase 4.2 → D | `shared/connections-editor`, capability flags, template "Connects to" | ok |

## Canvas coverage

(restructure, phase 4, no-auth) Rewritten for the current plan: 2.2 owners A–E first, then phase 4 and phase 3.

| Board | Owner | Elements owned elsewhere or later |
| --- | --- | --- |
| Shell on every board (rail, sidebars, tab bar, sheets, tooltip) | C | Counts from C's `/attention` (E fills parts; phase 4 adds approvals and blocked connections); the Credentials dot ", 1 expired" is phase 4.1; the account's role and Sign out are phase 3 |
| Factories, Mobile-Factories | C | Version line B; busy share E; tool-approval rows phase 4.2; New factory and Import targets D; weekly budget D |
| Org-Switcher, Mobile-Org-Switcher | C (data A: every organization on the install, Create organization) | The role in each row ("owner"), the "Members and roles" link and the account's role: phase 3; catalog and credentials links "Later" until phase 4 |
| Org-Settings | C (General, slots, Delete from A) | Danger zone stays (Delete); Transfer ownership is phase 3; Model access and Vault are "Arrives with credentials" placeholders until phase 4.1; vault move later (only PostgreSQL) |
| New-Start | D | Templates and creation B; "Connects to" B/phase 4.2 |
| New-Basics, Define-Basics | D | How the repository and model access sign in is read-only until phase 4.1's `CredentialSelect`; GitHub-connection picker phase 4.2; Who approves phase 3 |
| New-Line, Define (Line) | D | Line checks B; constraint/busy E; human stations decided by the local user (choosing approvers: phase 3); "Jira filter" and "GitHub issues" intake later (decision 11), read-only; "pnpm test adds about 2 minutes" later (needs check timings); DBR queue note "Not enforced yet" |
| New-Agents, Define-Agents | D | "Ask me" built-in tools warn until built-in approvals exist (later); "Insert" hidden (decision 5); "Open agent page" E |
| New-Review, Define-Publish | D | Checks/compare/publish B; slots A; phase 4.2 checks; dry-run checkbox later (Jonah) |
| Define-YAML | D | Ranges, problems, format B; connection rules phase 4.2 |
| New-Connections, Define-Connections | D (section shell, the draft's connections list with `env:` sign-in lines, Remove) | The connector panel (credential select, tools table, tests, "Add from the catalog", problems with fixes): phase 4.2 |
| Main (Floor) | E | Rail and sidebar C; "Add an investigator" target D; Jira comment entry phase 4.2; "Knowledge sync failed" later; Jonah badge later; rework warning is display only (auto pull later) |
| Board | E | Rope sentence display only (DBR later); expedite limit display only; chrome C; Connections dot phase 4.2 |
| Item | E | "From a Grafana alert" chip: connector intake, **unowned → later** (decision 11); Knowledge attached later; Draft diagnosis = latest agent message; Note E14a/E14b; credential link on a blocked item phase 4.1; full transcript later |
| Agent | E (frame, stats, reasons) | Skills and Tool policy cards, Edit in Definition D; Connections card phase 4.2; "See the proposed skill change" later (Jonah) |
| Mobile-Floor, Mobile-Board, Mobile-Item, Phone | E | Phone chrome C; "Jonah proposes a fix" later |
| Mobile-Agent | E (stats) | Cards D, Connections card phase 4.2 |
| Credentials | Phase 4.1 | Connector columns, OAuth/App/STS rows phase 4.2 |
| Credential-New | Phase 4.1 (step 3, save) | Steps 1–2, OAuth, connector fields phase 4.2 |
| Catalog | Phase 4.2 | Chrome C |
| Connections | Phase 4.2 | Sidebar dot C |
| Approval | Phase 4.2 | Item timeline E |
| Members | Phase 3 (no-auth) | C builds no route; the entry is absent from the org menu and sidebar |
| Knowledge, Mobile-Knowledge | later | C shows the entry as "Later" |
| Jonah, Remediation, Mobile-Jonah | later (Jonah) | C shows the entry as "Later" |

## Scope decisions

| Item | Spec | Decision | Reason |
| --- | --- | --- | --- |
| Drum-buffer-rope release (`ReleaseUpTo`, rope) | E | Deferred (later) | Unscheduled flow-control work; the Board only needs the configured policy and counts |
| Expedite-lane WIP enforcement | E | Deferred (later) | Part of release; "1 of 1 in use" is a count |
| Automatic rework-rate andon pull | E | Deferred (later) | Flow-control work; the board shows a warning and a manual cord |
| Manual andon pull/clear stopping the line | E | Kept | Canvas button must act |
| Detected-constraint cache feeding the engine | E | Deferred (later) | Only DBR uses it; the Floor's constraint report stays a read model |
| Constraint report, lead-time bridge, busy share, `WaitSaved` estimate | E | Kept | Main board shows them (display) |
| Run transcripts in the bucket | E | Deferred; needs an ADR | AGENTS.md rule 3; no board shows a full transcript |
| Live notes over stdin (E14b) | E | Kept as a separable PR | The canvas shows notes to a running agent; E14a (next run) gives the behaviour without the worker change (decision 8) |
| Weekly budget enforcement | D | Kept | New-Review/Define-Publish expose drain/stop; a setting that does nothing would mislead |
| Org and per-factory slot caps | A | Kept | Org-Settings shows them ("factories wait for each other at peak") |
| Gateway multi-instance forwarding | Phase 4.2 | Deferred to the AWS deployment | One goald in 2.2; architecture.md already documents the constraint |
| MCP Registry import, OAuth, CLI sidecars, GitHub App and STS minting | Phase 4.2 | Kept | Catalog, Credential-New, Credentials and Connections boards show them |
| OIDC sign-in, sessions, API tokens, roles | A | Moved to phase 3 (no-auth) | User decision: options such as Better Auth are evaluated first |
| Credentials, connectors, MCP gateway | phase 4 | Out of 2.2 (phase 4) | User decision |
| KMS adapter, Bedrock, Secrets Manager, vault move | phase 4.1 | Out of phase 4 | Need the AWS deployment (later) |
| Connector intake (Jira filter, GitHub issues, alert source) | none | Later (decision 11) | No spec owns it; it is new engine work |
| Drop of `factories.definition` | B | Moved to 2.3 | E's backfill depends on it |

## Architecture findings

| Rule | Spec | Status |
| --- | --- | --- |
| RLS + `org_id` on every org table, A's kit | B, phase 4.1, E, phase 4.2 | Fixed: B, phase 4.1, E and phase 4.2 used `current_setting` directly, other policy names, no `DEFAULT goal_org()`, no cascade, single-column FKs |
| `goal_org()` fails closed when unset | Phase 4.2 | Fixed: Phase 4.2's tolerant version renamed and confined to `connectors` |
| Hexagonal: engine and domain import only ports | all | OK; recommend turning the engine depguard rule into an allow-list (README) |
| api imports no adapters or core services directly | B, phase 4.1, D, phase 4.2 | Fixed: `api` denies `authoring`, `factorymodel`, `secrets`, `builder`, `connectors` |
| New ports in `internal/ports`, wiring only in `cmd/*` | all | OK (`SkillSource`, `FlowStats`, `Minter`, `MCPRegistry`, … registered) |
| depguard rule for every new package | B, phase 4.1, D, phase 4.2 | OK after fixes (README table) |
| Port methods take the org from `ctx` | Phase 4.1, phase 4.2 | Fixed (convention; explicit `org` params dropped) |
| Stateless goald | D, B (caches), E (`Interrupt` map of own runs), phase 4.2 (sidecars, held approvals) | OK for one instance: caches are recomputable, and E's map is the cancel handle of runs it owns (heartbeat fallback). Phase 4.2's per-instance state is accepted only because forwarding is deferred, as architecture.md documents |
| PostgreSQL only; bucket only for definitions | E | Fixed: transcripts deferred. Catalogs (`catalog/templates`, `skills`, `connectors`) are definition content; the ADR 0005 amendment says so |
| No factory data in code or env | B (blank skeleton), phase 4.1 (kinds), D (`GOAL_MODELS`) | OK: blank comes from a catalog template; credential kinds and the model list are platform settings |
| Forward-only migrations | B, E | Fixed: B's drop moved after E's backfill; merge order rule (build sequence) |
| Composite FKs so a row cannot point across orgs | B, phase 4.1, E, phase 4.2 | Fixed in text (kit note); B's `agent_runs.definition_version_id` stays single-column, RLS covers it |
| Audit action format | E | Fixed |
| UI per MASTER.md | C | Fixed: new tokens go into MASTER.md first. MASTER's "rust dot = blocked" settles C Q7 |
| Tests protect behaviour | all | OK; E's tests for deferred features move with them |
| No employer names | all | OK (only `acme`, `example.com` and the canvas's own names) |
| Terraform / knowledge base in 2.2 | all | OK (KMS, IAM and KB stay designed or out; both are later work) |

## Build sequence

(restructure, phase 4, no-auth) Rewritten for 2.2 A–E with the slimmer A. Rule: a PR with a migration merges only after every lower-numbered migration has merged: 00004 A1 → 00005 A2 → 00006 A3 → 00007 A7 → 00010–11 B3 → 00030 E2 → 00031 E3 → 00032 E9 → 00033 E11. PRs without migrations follow only their own dependencies. Nothing in A–E waits for phase 4 or phase 3.

| Step | Lane 1 | Lane 2 | Lane 3 | Lane 4 | Blocks |
| --- | --- | --- | --- | --- | --- |
| 1 | A1 database roles (00004) | C1 api-client (on today's routes) | B4 config problems + schema + `SecretUses`, B5 factorymodel, B6 diff (pure) | B1 ADR amendment | — |
| 2 | A2 tenancy tables (00005) | | | | A3 |
| 3 | A3 org scope (00006) | B2 ports + fakes | E1 flow targets (after B4) | D2 purpose (after B4) | A4, B3 |
| 4 | A4 org-aware engine + bucket | | | | A5, B7 |
| 5 | A5 route move + seams (request actor, `Authorizer`) | | | | everything with API or web |
| 6 | A6 organization management, A7 slots (00007) | C2 shell API (`/attention`, `/overview`, `/inbox`) | C3 org routes + shell | B8 authoring (interim `EnvReferences` check) | B3 (after A7), C4, C5, C7, B9 |
| 7 | B3 definitions store (00010–11) | C4 org menu, C5 factories home | B9 definitions API | D3 skills catalog | B7, D4 |
| 8 | B7 engine pins versions + legacy import | C7 org settings (after A6; Model access and Vault placeholders) | B10 CLI on the API | D4 draft session, D5 mock (after B9) | E2, D6 |
| 9 | E2 item spans (00030) → E3 lease spans (00031) | D1 weekly budget | D6 frame + Basics (after C3, D4, D5) | | E4–E13, D7–D11 |
| 10 | E4 metrics, E5 bridge, E6 constraint report, E7 board stats | E10 move/pause/send back, E12 agent stats, E13 runs, E14a notes | E9 manual andon (00032) | D7 line, D8 agents, D9 connections shell, D10 YAML, D11 review | E8, E11, D12, D13 |
| 11 | E8 line display | E11 inbox kinds (00033, after E9, E10, C2) | D12 agent cards (after E12), D13 wizard | E14b live notes (optional) | 2.2 done |

C6 (Members) is not built (phase 3). **Phase 4 follows phase 3**: its design pass first, then its own sequence (4-credentials-and-connectors.md §15 of each part), with migrations 00050–51 (4.1-3) → 00052 (4.2-2) → 00053 (4.2-11) → 00054 (4.2-13). **Phase 3 (identity and access) comes first after 2.2**, with migrations 00040–00046 in its PR order (3-identity-and-access.md §15); phase 4 follows it.

Dependency violations found in the specs' own breakdowns and fixed (history): A8's migration was late (since no-auth, A has no 00007); B's 00011 sat in PR 7, behind the vault's migrations (moved to PR 3); E's backfill depended on a column B11 dropped (B11 moved to 2.3). Dependencies the specs left implicit, now in the table: D12 needs E12 (agent page frame); E1 after B4; B3 after A7 (number order). (restructure) D6 does not need `CredentialSelect`: phase 4.1-8 later replaces D's read-only Basics lines; `Secrets.Resolve` keeps its signature through A–E. Phase 4's own implicit dependencies (4.2-6 needs 4.1-5 and B7; 4.2-22 needs D9; 4.2-23 needs E13; 4.1-4 after B4) are noted in its spec.

## Questions answered

| Question (spec) | Answer | Source |
| --- | --- | --- |
| Factory roles ordered or separation of duties? (A1) | Moved to phase 3 (no-auth); the review's answer (ordered, editor ⊇ approver ⊇ viewer) is a starting point | Canvas Members: one segmented choice per factory |
| Email invitations? (A3, C4) | Moved to phase 3 (no-auth) | README "Out of 2.2" |
| Retention and bucket purge on org delete (A5) | 7 days, then A6's job writes delete markers on the org prefix; lifecycle rules come with the AWS deployment (later) | A6; phase scope |
| Slot caps owner (A6, phase 4.1 Q1, C1) | A (A7), shown in C's Settings | Canvas Org-Settings; A7 |
| Default org name (A7) | `default`/"Default", renamed in Settings or with `goal org update` | A §14 step 5 (non-interactive `goal up`) |
| API tokens shape (A8) | Phase 3 (no-auth) | README "Out of 2.2" |
| RP-initiated logout (A9) | Phase 3 (no-auth) | README "Out of 2.2" |
| Weekly budget enforcement (B1) | D §4.2 | D |
| Blob GC (B2) | Later; unreferenced blobs accumulate in 2.2 | B ADR amendment |
| Item vs run pinning (B3) | Runs pin; copy says "Runs already started finish on v8" | D §10.10 adopted the copy |
| Blank skeleton (B4) | A `blank` template in `catalog/templates/`; no names in Go | AGENTS.md rule 1 |
| Phase 4.2's `connections` table vs YAML (B5, phase 4.2 Q5) | YAML is the source; `definition_versions.resolved` pins bindings; `connections` is the current projection + health, written in B's publish tx | B §5.1, §6.1; Phase 4.2 §6 |
| Cross-org clone, org templates (B7) | Later | B §2 Out |
| Per-factory model access in Basics (phase 4.1 Q2) | Yes, under Advanced, once phase 4.1 adds `CredentialSelect` (read-only before phase 4, restructure) | D §10.5 |
| Repository via GitHub connection (phase 4.1 Q3) | Keep both forms (`{url, credential}` or `{connection, repo}`; `{url, token}` with an `env:` reference until phase 4); UI prefers the connection once phase 4.2 is wired | B §4.6 schema (oneOf) |
| Bedrock (phase 4.1 Q5) | With the AWS deployment | Phase scope |
| `GOAL_GITHUB_API_URL` enough (phase 4.1 Q6) | Yes for 2.2 | No GHES on any board |
| AWS Secrets Manager shown disabled (phase 4.1 Q7) | Disabled with its note | Canvas Org-Settings lists it |
| Tablet 768–1023 (C2) | Full sidebar per canvas, container queries | Canvas; MASTER "UI canvas is the reference" |
| Weekly budget source (C3, D5 part) | Per factory `runtime.budget.weeklyUsd`; org total = sum | B §4.6, D §4.2 |
| Old slugs (C5) | Same as A4, decision 2 | — |
| Factory marks (C6) | Derived client-side (S / Su) | Canvas marks match the derivation; no schema change |
| Rust dot for blocked items (C7) | Yes: blocked or andon | MASTER.md navigation note ("rust dot = blocked"); E §8.4 |
| Agent page ownership (D1) | E owns route and frame; D and phase 4.2 add cards | README registry |
| skills.sh search API (D2) | Start with curated `sources.yaml`; add `Search` only against a documented endpoint | D §5.1 fallback |
| Who approves inline (D6) | Moot in 2.2; phase 3 (no-auth) | README "Out of 2.2" |
| Capacity history for busy share (E2) | Accept the approximation in 2.2 (display metric) | Scope |
| "1 of 3" (E3) | Top three candidate constraints | Card text is per station ("Investigate is holding the line back") |
| Live draft output (E4) | Latest agent message until the output is submitted; no `goal__draft` tool | Scope |
| Move to cancel (E7) | Not in 2.2 | Not on any board |
| Transcript retention (E8) | Moot: transcripts deferred | Rule 3 |
| Laptop OAuth (phase 4.2 Q2) | Smoke tests decide; fallback is a static client per org (`oauth` method field) | Phase 4.2 §4.1 |
| Atlassian SSE vs `/v1/mcp` (phase 4.2 Q3) | Smoke test decides; SDK supports both | Phase 4.2 |
| Connector updates without publish (phase 4.2 Q4) | Never: versions pin at publish; deprecate + warning | B's version model |
| Exec sidecars need coreutils (phase 4.2 Q6) | Accept for v1 (aws-cli and azure-cli ship it) | Both CLIs on the canvas qualify |
| Hosted catalog operator (phase 4.2 Q7) | Later (hosted deployment is unscheduled) | Phase scope |
| goose allow-missing vs merge order (review) | Merge in number order; keep goose's default | Forward-only safety; no code change |
| `JobLocks` port (phase 4.2 needs) | Not needed: each job's store method takes `pg_try_advisory_xact_lock` | Phase 4.1's `Rewrap` pattern; architecture "Singletons" |

## Decisions for the user

**Decided 2026-10-23:** 2.2 is built on the Recommendation column for decisions 2, 3, 5, 6, 7, 8, 11 and 12 (so A5 adds `net/http.CrossOriginProtection`). Changing one later is its own change.

(no-auth, phase 4) Decision 1 moved to phase 3 and decision 9 is moot in 2.2: both are about who may do something, and 2.2 has one local user. Decisions 4 and 10 belong to phase 4's design pass.

| # | Question | Recommendation | Alternative |
| --- | --- | --- | --- |
| 1 | **Moved to phase 3** (no-auth): who may create organizations once people sign in (was A2). In 2.2 the local user creates them. | Phase 3 starting point: any signed-in user allowed by the identity provider's policy | Operator only |
| 2 | Renaming an org slug (A Q1, C5) | Accept the break, with C's warning "Links with the old address stop working" | Keep an `organization_slugs` history and redirect |
| 3 | Restore semantics (B6) | Pointer move (instant rollback, numbers never reused; history shows "Running" on the restored version) | Restore creates version N+1 with the old manifest |
| 4 | Credential rotation (phase 4.1 Q4): **phase 4's design pass** (credential lifecycle), not decided here (restructure) | Starting point: erase the previous ciphertext at rotation (crypto-shred; runs keep what they resolved) | Keep it 24 h with an undo endpoint |
| 5 | Prompt "Insert" buttons (D3) | Hide them in 2.2 | Add `{{item.*}}` placeholders in the worker and B's validation |
| 6 | YAML editor (D4) | Keep Monaco (ADR 0001, frontend.md) with D's phone textarea | ADR to switch to CodeMirror 6 before D8 |
| 7 | Budget and metrics time base (D5, E1) | Rolling 7 days and UTC days in 2.2; `dailyUsd` stays display-only | Calendar week in an org time zone (adds an org setting) |
| 8 | Live notes to a running agent (E5) | Ship E14a (next run) in 2.2; E14b (stdin, streaming input) only if time allows | Build both now, or drop `TaskInput` |
| 9 | **Moot in 2.2** (no-auth): who may pull the andon cord (E6). The local user pulls and clears; phase 3 decides who may. | Phase 3 starting point: anyone who can see the line pulls; clearing needs a decision-maker | Restrict pulls |
| 10 | Held tool approvals (phase 4.2 Q1): **phase 4's design pass** (approval-gated tools) | Starting point: keep the run, container and WIP slot for up to 30 min (`approvalTimeout`) | Suspend the run and requeue on decision (worker resume + engine state) |
| 11 | Connector intake (Jira filter, GitHub issues, alert source chip): no spec owns it | Later: show intake read-only with "Edit in YAML" and hide the source chip when absent | Add an intake work package to phase 4.2 |
| 12 | (no-auth) Cross-origin protection on the loopback API (A Q4): it moved to phase 3 with CSRF, so a web page can still send requests to goald on 127.0.0.1, as today | Add `net/http.CrossOriginProtection` in A5 now (no identity involved) | Wait for phase 3 |

## Not resolved here

- Exact rewrites of every SQL block in B, phase 4.1, E and phase 4.2 to the tenancy kit: the specs carry a binding kit note per migration, and the guard test `TestSchema_EveryOrgTableHasRLS` enforces it. The literal SQL is fixed in the PRs.
- Phase 4.1's and phase 4.2's port signatures still show explicit `org` parameters in the code blocks; the README convention and a note at each port say to drop them.
- Whether the canvas's "1 of 3" pages candidates or insights cannot be confirmed from the board's text; E's reading was kept.
