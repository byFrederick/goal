# Phase 2.2 cross-spec review

Scope: the seven specs as they were then (A–G; now A–F, see the next section), README conventions, AGENTS.md, architecture, testing, frontend, ADRs 0001–0006, MASTER.md and the 37 canvas boards. Every change made in a spec ends with "(review)".

## Restructure after review

(restructure) After this review the user decided that credentials and the vault belong with connectors, and that the whole area should be phased out to the end so it can be designed carefully. The plan changed as follows; the findings below are kept as history, with phase letters, PR ids, sections and migration numbers updated to the new plan.

| New | Spec | Was |
| --- | --- | --- |
| A | Organizations and identity (`2.2-a-organizations.md`) | A |
| B | Definitions and versions (`2.2-b-definitions.md`) | B |
| C | App shell and organization screens (`2.2-c-app-shell.md`) | D |
| D | Factory builder (`2.2-d-factory-builder.md`) | E |
| E | Work screens and flow metrics (`2.2-e-work-screens.md`) | F |
| F | Credentials, connectors and MCP gateway (`2.2-f-credentials-and-connectors.md`): part F1 vault and credentials, part F2 connectors and MCP gateway | C + G |

- **One last phase.** F merges the old vault spec (F1, PRs `F1-1…F1-9`, migrations `00040–00041`, were `00020–00021`) and the old connectors spec (F2, PRs `F2-1…F2-26`, migrations `00042–00044`, were `00040–00042`). It opens with a design pass (threat model, credential lifecycle, OAuth and MCP authorization, secret storage options, per-run minting, approval-gated tools, connector packaging) that may rewrite it. OpenAPI tags: `credentials` (was `vault`) and `connectors`.
- **Interim rule until F (README).** Model access (`GOAL_TASK_ENV` → `CLAUDE_CODE_OAUTH_TOKEN`) and repository access (`env:GOAL_GITHUB_TOKEN`) stay platform secrets of the install, resolved through today's `ports.Secrets` env adapter, as a marked, temporary exception to "nothing about a factory is an environment variable". Definitions keep their `env:` references. B validates only that each `env:` reference names a set variable that is not one of goald's own settings, and refuses `credential:`; no grant checks.
- **Stripped from A–E** (each edit marked "(restructure)"): A's settings carry no model-access or vault fields; B's `credentials` check runs on the interim `EnvReferences` behind `authoring.Credentials` (no vault types), and `FactoryModel` loses the credential fields until F1; C's Org settings show Model access and Vault as "Arrives with credentials", `OrgAttention` loses `credentials_needing_attention`, the Credentials entry has no dot, C7 no longer needs vault kinds; D's Basics shows how the repository and model access sign in read-only (new `capabilities.credentials` flag), D6 no longer waits for `CredentialSelect`, the mock has no credentials; E shows block reasons as text, and `credential_unavailable` arrives with F1.
- **What moved under F's design pass:** decision 4 (credential rotation), F1's open questions, and the review of held tool approvals (decision 10 keeps its default for now).
- **Why it fits.** No phase before F imports a vault type or calls a vault endpoint; every A–E pull request can merge without F; F plugs into named extension points (`authoring.Credentials`, `capabilities.credentials`, C's settings placeholders and sidebar, E's block reason).

## Verdict

**Buildable after the fixes below.** The seven specs (six since the restructure) now agree on names, paths, tables, events, permissions and ports, and the build order below has no cycles. The plan respects the architecture rules once the scope cuts are applied: drum-buffer-rope, the automatic andon pull and the constraint cache move to phase 4; run transcripts move out (bucket rule); multi-instance gateway forwarding moves to the AWS deployment.

Two things a builder must keep in mind:
1. **Migration PRs merge in number order.** goose (`NewProvider`, default settings) refuses a lower-numbered migration that arrives after a higher one is applied. Each phase's other PRs can run in parallel.
2. **Ten product decisions** remain open (last section; decision 4 moved to F's design pass, restructure). Each has a recommended default, so building can start on those defaults.

Fixes: 48 rows in the table below, made as 182 marked edits across the README and the seven specs. By type: contracts 22, collisions 9, architecture 9, scope 6, build order 2.

## Fixes applied

| Spec | Section | Change | Why |
| --- | --- | --- | --- |
| README | Registry | Registered every new port, package, route, query-key root, stream event, NOTIFY channel, token claim, bucket prefix, migration number, shared schema; added the package/depguard table | Rule 2: one registry; specs had introduced ~40 unregistered names |
| README | Registry | Problem `code` is kebab-case; audit actions `noun.past_verb`; org from `ctx`; permissions only in A's table | B and D used kebab-case, F1 and F2 snake_case; E's audit names failed A's CHECK regex; F1 and F2 passed `org` explicitly |
| A | §5.1, §8.5, §16 | New `goal_org_events` channel and `ItemNotice.Org` for non-item stream events; the feed filters every event by org and visible factory | F1's `credential` event relied on an "org-event NOTIFY" A did not have; F2 invented `goal_connections` |
| A | §5.1 | `api.Factories` listing moves to B's `api.Definitions` | A said "B keeps this", B removed it |
| A | §3, §8.2, §16 | `GET /settings` needs `org.read`; model access and vault live on F1's `/settings/vault`; C's Save sends both requests | C shows Settings read-only to members; D needs slot caps; A and F1 disagreed on one vs two endpoints |
| A | §8.7 | Added `UserRef`, `Problem.code` | B needed `UserRef`; B, F1, D, F2 need `code` |
| A | §10 | C's `subscribe(baseUrl, {org, factory?}, handlers)`; keys use the org id | A and C had different signatures; C keys by id |
| A | §11.2 | `credentials.read` → admin; added `andon.pull` (viewer), `item.move/pause/note`, `andon.clear` (approver), `factory.pause` (editor); `item.retry` → approver; `connections.bind` → `connections.test`; `factory.delete` marked later | F1's list is admin-only; E's operator actions had no rows; Pause/Resume need the same role; F2 binds through definition edits |
| A | §6.1, §6.6 | `goal_org()` stays strict, F2 adds `goal_org_or_null()`; RLS test allowlist gains `vault_keys`; `connectors` named policies | F2 had redefined `goal_org()`; F1's platform table and F2's curated rows would fail the guard test |
| A | §15 | A8 split: A8a sessions migration 00007 right after A7; A8b OIDC (no migration) any time later | 00007 landing after B's 00010 breaks goose |
| B | §2, §17 | Weekly budget enforcement is D's | D took it (B Q1) |
| B | §4.7, §5.3, §16 | `authoring.Credentials` = F1's `Vault.Check`; `authoring.Connectors`/`ConnectorInfo` dropped; F2's checker is an `authoring.Check` + `authoring.Resolver`; `Members` = A's `Tenancy.Members` | B needed `CredentialStates` and `Connectors(refs)`, which F1 and F2 don't provide; A has no `Approvers` |
| B | §5.1, §6.1 | `Publish.Resolved`, `Publish.Bindings`; `definition_versions.resolved jsonb` | Resolves B Q5 and F2 Q5: runs on older versions read their pinned bindings; B's tx projects `connections` |
| B | §6.1 | A's tenancy kit (`DEFAULT goal_org()`, `ON DELETE CASCADE`, `org_isolation` with `(SELECT goal_org())`) | Rule: RLS + org_id per A; purge needs cascade |
| B | §6.1, §14, §15 | `00012` (drop `factories.definition`) moved to 2.3; `00011` moved into PR 3 | E's 00030 backfill reads that column and runs after 00012 on a fresh DB; migration order |
| B | §7, §14 | Legacy import reads A's interim `orgs/<org>/factories/<id>/`, not `factories/<id>/` | A4 moves and deletes the old keys before B runs |
| B | §8.1, §8.3, §16 | `GET /factories` is one operation returning A's `Factory` + `current/draft/paused`; `FactorySummary` gone; roles mapped to permissions | Path defined twice (A, B); `FactorySummary` collided with C's schema |
| B | §8.2 | `Validation.problems` typed `DefinitionProblem`; FactoryModel gains `repository.credential`, `model_credential`, `skills[].origin`, `flow.targets` | D §16 asked for them (F1's, D's, E's fields) |
| B | §8.4, §16, §18 | Stream event union owned by C, feed by A; ADR amendment lists all three catalogs | Owner was "E/C"; rule 3 needed the catalogs named |
| F1 | §5.2 | Org from `ctx`, explicit `org` params dropped | README convention (A) |
| F1 | §6.1, §6.2 | Tenancy kit, `org_isolation` names, cascade, composite `credential_uses.run_id` | A's guard test requires `org_isolation`; purge |
| F1 | §6.6 | `credentials.connector`/`method` text (not `connector_id`), `needs_reconnect`, sealing inside the postgres adapter, `Resolve`/`Rotate` mapping for F2 | F2's needs (`AddVersion`, `SetStatus`, `MarkUsed`, `Seal/Open`) mapped onto F1's port without new core methods except `MarkNeedsReconnect` |
| F1 | §8 | Roles → permissions; kebab codes; `GET /settings/vault` is `org.read`; SSE via `goal_org_events` | C's read-only settings; channel gap |
| F1 | §10.1 | Route files under `o.$org._org` | C's org-sidebar layout |
| C | §2, §8, §10, §15, §16 | `/factories/summary` → `/overview` (`FactoryOverview[List]`); `/waiting` → `/inbox` shared with E (`InboxSource` registry in C2) | `summary` shadowed a factory named "summary"; C and E each built an inbox |
| C | §4, §5 | One `domain.AndonPull` (E extends); `Store.WaitingHuman` interim until E's `InboxItems` | Duplicate type; same postgres type implements both ports |
| C | §8 | `budget_usd` from `weeklyUsd` | B/D schema (C Q3) |
| C | §10.8, §10.9, §16 | Members and invitations separate queries; settings PATCH on `/settings` + F1's vault endpoint; slots from A7 | A's actual endpoints |
| C | §10.10 | `invalidationsFor` names work keys one by one; never the bare factory prefix | Item bursts would refetch B's drafts and F1's factory credentials (D's finding) |
| C | §10.1, §16 | `/agents/:agent` owned by E; Credentials pages by F1 | Two specs claimed the route; "F1 or F2" |
| C | §10.11 | New tokens go into MASTER.md first | Rule 10; tokens are generated from MASTER.md |
| D | §10.6 | DBR queue sentence and Flow panel say "enforced from phase 4" | Scope cut in E |
| D | §5.2 | `api` rule denies `internal/builder` | Boundary listed for every new package |
| D | §16 | Query-key issue resolved; slots via `GET /settings`; `builtins: ask` stays a warning | Fixed in C/E; A change; F2 gates gateway tools only |
| E | §1, §2 | Deferred list: DBR release, expedite limit, automatic rework pull, constraint cache, transcripts | Roadmap phase 4; rule 3 |
| E | §4.3, §4.4, §4.7, §7 | Marked deferred; manual andon still stops the line | Canvas needs "Pull the andon cord" to act |
| E | §4.5 | `by domain.Actor` on every engine method | A §4.2 |
| E | §5.1, §5.2 | `FlowStats.Attention` → `InboxItems`; `ReleaseUpTo`, `SetRunTranscript` deferred | Method-name clash with C's `Store.Attention` on one Go type |
| E | §6 | Tenancy kit, composite parent keys; backfill tolerates NULL `factories.definition` | Rule; B stops writing that column at B7 |
| E | §8 | Roles → permissions; `Board.line.release.enforced`, `budget`; `ItemCard.pending_approval`; `/inbox` is C's without `factories[]`; F2's `ToolApproval`; no `by` | Contracts with A, C, D, F2 |
| E | §10.3, §11 | Explicit invalidation keys; audit names `andon.pulled`, … ; no transcript read audit | Collision; A's CHECK regex |
| E | §13, §15 | E8 display only, E9 manual andon, E11 extends C's inbox, E13 runs without transcripts, E14 split a/b; deferred tests noted | Scope |
| F2 | §1, §2, §4.5, §4.7, §5.3, §14, §15 | Forwarding (`g` claim, `GOAL_PEER_URL`, F2-16) deferred to the AWS deployment | One goald in 2.2; no board needs it |
| F2 | §4.3 | Checker = `authoring.Check` + `Resolver`; kebab codes; three codes merged with B's; `blocked` severity → warning | B's pipeline shape and severities |
| F2 | §5.1, §5.5 | Org from `ctx`; sealing in the adapter; `connectors` may import `authoring`; `api` denies `connectors` | Conventions, depguard |
| F2 | §6 | `goal_org_or_null()` only for `connectors`; tenancy kit elsewhere; adds F1's credential columns; `connection` event on `goal_org_events` | A's strict helper; channel registry |
| F2 | §8, §10 | Roles → permissions; query keys under `['org', id]` | A's table; C's root |
| F2 | §16 | Needs from A, B, F1, C/E rewritten to what those specs provide; no `JobLocks` port | Contract mismatches |

## Contract matrix

| Provider → consumer | Interface | Status |
| --- | --- | --- |
| A → all | `domain.OrgID/UserID/Actor`, `WithOrg/OrgFrom`, `ErrNoOrg`, tenancy SQL kit, `goal_owner`/`goal_app` | ok |
| A → all | Permission table (A §11.2), `x-goal-permission`, `s.factory/s.item` resolvers | fixed (rows for B, F1, E, F2) |
| A → B, F1, E, F2 | `Tenancy.ActiveOrgs` for cross-org jobs (refresh, rewrap, sweeps) | ok (answers F1 and F2 needs) |
| A → B | `Tenancy.Members` for station approvers | fixed (was `Approvers`) |
| A → C | `/me`, `/orgs`, `/orgs/{org}`, `/settings`, `/members`, `/invitations`, `/transfer`, DELETE org | fixed (C's paths and shapes) |
| A → D | `GET /settings` slots readable by members | fixed |
| A ↔ C | `subscribe(baseUrl, {org, factory?}, handlers)` | fixed |
| A → all | `Problem.code` (kebab-case), `UserRef` | fixed |
| A → F1, F2 | `goal_org_events` NOTIFY, `ItemNotice.Org` | fixed (new) |
| A → F2 | `goal_org()` strict; F2's own `goal_org_or_null()` | fixed |
| A, B → F2 | Task token claims `o`, `v` | ok (`g` deferred) |
| B → A, C, D | `GET /factories` → `Factory` (+`current`, `draft`, `paused`) | fixed (collision) |
| B → D | Draft/model/files/validate/publish/versions/compare/templates API, `defKeys`, `FactoryModel` | fixed (model additions) |
| B → C, D, E | `definition` stream event from `goal_definitions` | fixed (owner) |
| B → E | `agent_runs.definition_version_id`, `moved` with `reason`, `paused` | ok |
| B → F2 | `definition_versions.resolved`, `Publish.Bindings` → `connections` rows in the publish tx | fixed (new) |
| B → F1 | Draft/publish to rewrite references at cutover | ok (F1-9 comes long after B10) |
| B → F1 | `authoring.Credentials` extension point, `config.Factory.SecretUses()`; interim `EnvReferences` until F | restructure (no vault types in B) |
| F1 → B | `authoring.VaultCredentials` over `Vault.Check(ctx, factory, uses)` | fixed (was `CredentialStates`); arrives with F (restructure) |
| F1 → C | `/settings/vault` replacing C's placeholders; `credentials_needing_attention` added to `/attention` from `Vault.List` statuses (admins; 0 for others) | fixed; arrives with F (restructure) |
| F1 → D | `CredentialSelect`, `GET /factories/{f}/credentials`, `credential` event, `capabilities.credentials` | arrives with F; D ships read-only lines (restructure) |
| F1 → E | Block `reason_code: credential_unavailable` linked to the credential | arrives with F (restructure) |
| F1 → F2 | `Vault.Resolve`/`Rotate`/`Credential`, `NewCredential.Connector/Method`, `MarkNeedsReconnect`, `envelope.go` in-adapter | fixed (mapped) |
| C → B, F1, D, E, F2 | Route tree, `screens.ts`, `streamHandlers`, `invalidationsFor`, UI inventory | fixed (explicit keys) |
| C → E, F2 | `/attention`, `/inbox` + `InboxSource` registry, `domain.AndonPull` | fixed (merged with E's inbox) |
| C → B, E, F2 | `/overview` (`FactoryOverview`) | fixed (renamed) |
| D → B, E, C | Weekly budget enforcement, `Store.SpendSince`, `Board.line.budget` | ok (E's Board field added) |
| D → E, F2 | Agent page cards; `shared/connections-editor` slot; `/definition/*` deep links | ok |
| E → C | Inbox kinds, `FlowStats.InboxItems`, extended `AndonPull`, `event: factory` | fixed |
| E → D | `flow.targets`, Agent page frame and route | ok (route ownership fixed) |
| E → F2 | `ItemCard.pending_approval`, item timeline | fixed (accepted) |
| F2 → B | `connectors.Checker` as `authoring.Check`/`Resolver`, problem codes | fixed |
| F2 → C, E | `tool_approval` `InboxSource`, `/approvals`, `approval`/`connection` events, `connections_blocked` | fixed |
| F2 → D | `shared/connections-editor`, capability flags, template "Connects to" | ok |

## Canvas coverage

(restructure) Rewritten for the new order: rows go by owning phase in build order.

| Board | Owner | Elements owned elsewhere or later |
| --- | --- | --- |
| Shell on every board (rail, sidebars, tab bar, sheets, tooltip) | C | Counts from C's `/attention` (E and F fill parts); the Credentials dot ", 1 expired" arrives with F1 |
| Factories, Mobile-Factories | C | Version line B; busy share E; tool-approval rows F2; New factory and Import targets D; weekly budget D |
| Org-Switcher, Mobile-Org-Switcher | C | Data A; catalog and credentials links "Later" until F |
| Members | C | Endpoints A; role copy static |
| Org-Settings | C | General, slots, transfer, delete A; Model access and Vault are "Arrives with credentials" placeholders until F1 fills them; vault move later (only PostgreSQL in 2.2) |
| New-Start | D | Templates and creation B; "Connects to" B/F2 |
| New-Basics, Define-Basics | D | How the repository and model access sign in is read-only until F1's `CredentialSelect`; GitHub-connection picker F2; Who approves A |
| New-Line, Define (Line) | D | Line checks B; constraint/busy E; "Jira filter" and "GitHub issues" intake later (decision 11), read-only; "pnpm test adds about 2 minutes" later (needs check timings); DBR queue note "from phase 4" |
| New-Agents, Define-Agents | D | "Ask me" built-in tools warn until built-in approvals exist (later); "Insert" hidden (decision 5); "Open agent page" E |
| New-Review, Define-Publish | D | Checks/compare/publish B; slots A; F2 checks; dry-run checkbox later (Jonah) |
| Define-YAML | D | Ranges, problems, format B; connection rules F2 |
| Main (Floor) | E | Rail and sidebar C; "Add an investigator" target D; Jira comment entry F2; "Knowledge sync failed" later (phase 3); Jonah badge later; rework warning is display only (auto pull phase 4) |
| Board | E | Rope sentence display only (DBR phase 4); expedite limit display only; chrome C; Connections dot F2 |
| Item | E | "From a Grafana alert" chip: connector intake, **unowned → later** (decision 11); Knowledge attached later; Draft diagnosis = latest agent message; Note E14a/E14b; credential link on a blocked item F1 |
| Agent | E (frame, stats, reasons) | Skills and Tool policy cards, Edit in Definition D; Connections card F2; "See the proposed skill change" later (Jonah) |
| Mobile-Floor, Mobile-Board, Mobile-Item, Phone | E | Phone chrome C; "Jonah proposes a fix" later |
| Mobile-Agent | E (stats) | Cards D/F2 |
| Credentials | F1 | Connector columns, OAuth/App/STS rows F2 |
| Credential-New | F1 (step 3, save) | Steps 1–2, OAuth, connector fields F2 |
| Catalog | F2 | Chrome C |
| Connections | F2 | Sidebar dot C |
| New-Connections, Define-Connections | F2 (panel) | Section shell D (pre-F list with `env:` sign-in lines); problems B/F2 |
| Approval | F2 | Item timeline E |
| Knowledge, Mobile-Knowledge | later (phase 3) | C shows the entry as "Later" |
| Jonah, Remediation, Mobile-Jonah | later (Jonah) | C shows the entry as "Later" |

## Scope decisions

| Item | Spec | Decision | Reason |
| --- | --- | --- | --- |
| Drum-buffer-rope release (`ReleaseUpTo`, rope) | E | Deferred to phase 4 | Roadmap phase 4; the Board only needs the configured policy and counts |
| Expedite-lane WIP enforcement | E | Deferred to phase 4 | Part of release; "1 of 1 in use" is a count |
| Automatic rework-rate andon pull | E | Deferred to phase 4 | Roadmap andon; the board shows a warning and a manual cord |
| Manual andon pull/clear stopping the line | E | Kept | Canvas button must act |
| Detected-constraint cache feeding the engine | E | Deferred to phase 4 | Only DBR uses it; the Floor's constraint report stays a read model |
| Constraint report, lead-time bridge, busy share, `WaitSaved` estimate | E | Kept | Main board shows them (display) |
| Run transcripts in the bucket | E | Deferred; needs an ADR | AGENTS.md rule 3; no board shows a full transcript |
| Live notes over stdin (E14b) | E | Kept as a separable PR | The canvas shows notes to a running agent; E14a (next run) gives the behaviour without the worker change (decision 8) |
| Weekly budget enforcement | D | Kept | New-Review/Define-Publish expose drain/stop; a setting that does nothing would mislead |
| Org and per-factory slot caps | A | Kept | Org-Settings shows them ("factories wait for each other at peak") |
| Gateway multi-instance forwarding | F2 | Deferred to the AWS deployment | One goald in 2.2; architecture.md already documents the constraint |
| MCP Registry import, OAuth, CLI sidecars, GitHub App and STS minting | F2 | Kept | Catalog, Credential-New, Credentials and Connections boards show them |
| OIDC sign-in | A | Kept (A8b, last) | README decision; local mode unaffected |
| API tokens (A9), KMS adapter, Bedrock, Secrets Manager, vault move | A, F1 | Out of 2.2 | Not on boards / need the AWS deployment |
| Connector intake (Jira filter, GitHub issues, alert source) | none | Later (decision 11) | No spec owns it; it is new engine work |
| Drop of `factories.definition` | B | Moved to 2.3 | E's backfill depends on it |

## Architecture findings

| Rule | Spec | Status |
| --- | --- | --- |
| RLS + `org_id` on every org table, A's kit | B, F1, E, F2 | Fixed: B, F1, E and F2 used `current_setting` directly, other policy names, no `DEFAULT goal_org()`, no cascade, single-column FKs |
| `goal_org()` fails closed when unset | F2 | Fixed: F2's tolerant version renamed and confined to `connectors` |
| Hexagonal: engine and domain import only ports | all | OK; recommend turning the engine depguard rule into an allow-list (README) |
| api imports no adapters or core services directly | B, F1, D, F2 | Fixed: `api` denies `authoring`, `factorymodel`, `secrets`, `builder`, `connectors` |
| New ports in `internal/ports`, wiring only in `cmd/*` | all | OK (`SkillSource`, `FlowStats`, `Minter`, `MCPRegistry`, … registered) |
| depguard rule for every new package | B, F1, D, F2 | OK after fixes (README table) |
| Port methods take the org from `ctx` | F1, F2 | Fixed (convention; explicit `org` params dropped) |
| Stateless goald | D, B (caches), E (`Interrupt` map of own runs), F2 (sidecars, held approvals) | OK for one instance: caches are recomputable, and E's map is the cancel handle of runs it owns (heartbeat fallback). F2's per-instance state is accepted only because forwarding is deferred, as architecture.md documents |
| PostgreSQL only; bucket only for definitions | E | Fixed: transcripts deferred. Catalogs (`catalog/templates`, `skills`, `connectors`) are definition content; the ADR 0005 amendment says so |
| No factory data in code or env | B (blank skeleton), F1 (kinds), D (`GOAL_MODELS`) | OK: blank comes from a catalog template; credential kinds and the model list are platform settings |
| Forward-only migrations | B, E | Fixed: B's drop moved after E's backfill; merge order rule (build sequence) |
| Composite FKs so a row cannot point across orgs | B, F1, E, F2 | Fixed in text (kit note); B's `agent_runs.definition_version_id` stays single-column, RLS covers it |
| Audit action format | E | Fixed |
| UI per MASTER.md | C | Fixed: new tokens go into MASTER.md first. MASTER's "rust dot = blocked" settles C Q7 |
| Tests protect behaviour | all | OK; E's tests for deferred features move with them |
| No employer names | all | OK (only `acme`, `example.com` and the canvas's own names) |
| Terraform / knowledge base before phase 3 | all | OK (KMS, IAM and KB stay designed or out) |

## Build sequence

(restructure) Rewritten for the new order. Rule: a PR with a migration merges only after every lower-numbered migration has merged: 00004 A2 → 00005 A3 → 00006 A7 → 00007 A8a → 00010–11 B3 → 00030 E2 → 00031 E3 → 00032 E9 → 00033 E11 → 00040–41 F1-3 → 00042 F2-2 → 00043 F2-11 → 00044 F2-13. PRs without migrations follow only their own dependencies. Phases A–E build without any F pull request.

| Step | Lane 1 | Lane 2 | Lane 3 | Lane 4 | Blocks |
| --- | --- | --- | --- | --- | --- |
| 1 | A1 roles | C1 api-client (on today's routes) | B4 config problems + schema + `SecretUses`, B5 factorymodel, B6 diff (pure) | B1 ADR amendment | — |
| 2 | A2 tenancy (00004) | | | | A3 |
| 3 | A3 org scope (00005) | B2 ports + fakes | E1 flow targets (after B4) | D2 purpose (after B4) | A4, B3 |
| 4 | A4 org-aware engine + bucket | | | | A5, B7 |
| 5 | A5 routes + authz | | | | everything with API or web |
| 6 | A6 members/settings | C2 shell API (`/attention`, `/overview`, `/inbox`) | C3 org routes + shell | B8 authoring (interim `EnvReferences` check) | A7, C4–C7, B9 |
| 7 | A7 slots (00006) → A8a sessions (00007) | C4 org menu, C5 factories home | B9 definitions API | D3 skills catalog | B3 (migration order) |
| 8 | B3 definitions store (00010–11) | C6 members, C7 org settings (Model access and Vault placeholders) | A8b OIDC (any time from here) | D4 draft session, D5 mock (after B9) | B7, D6 |
| 9 | B7 engine pins versions + legacy import | B10 CLI on the API | D6 frame + Basics (after C3, D4, D5) | | E2, D1, D7–D11 |
| 10 | E2 item spans (00030) → E3 lease spans (00031) | D1 weekly budget | D7 line, D8 agents, D9 connections shell, D10 YAML, D11 review | | E4–E12 |
| 11 | E4 metrics, E5 bridge, E6 constraint report, E7 board stats | E10 move/pause/send back, E12 agent stats, E13 runs, E14a notes | E9 manual andon (00032) | | E8, E11, D12, D13 |
| 12 | E8 line display | E11 inbox kinds (00033, after E9, E10, C2) | D12 agent cards (after E12), D13 wizard | E14b live notes (optional) | F design pass |
| 13 | **F design pass** (may rewrite F; the steps below follow the current F spec) | | | | all of F |
| 14 | F1-1 secret scope | F1-2 vault domain, keyfile, envelope | F1-4 credential schema and config | F2-1 connector package, F2-10 OAuth adapter, F2-17 registry | F1-3, F1-5 |
| 15 | F1-3 postgres vault (00040–41) | | | | F1-5, F2-2 |
| 16 | F1-5 resolution + `VaultCredentials` | F1-6 credentials API + probes | F2-2 catalog (00042) → F2-3 sync, F2-4 catalog API | F2-9 exec sidecars, F2-19 web catalog | F1-7, F1-8, F2-5, F2-11 |
| 17 | F1-7 CLI + import-env | F1-8 web credentials (fills C's settings and dot, D's `CredentialSelect`, E's link) | F2-5 connector connections + B publish hook → F2-6 gateway | F2-11 OAuth service (00043) → F2-12 credential tests | F1-9, F2-7, F2-13, F2-20 |
| 18 | F1-9 cutover (after F1-7 and the goal factory's migration) | F2-7 GitHub App minting, F2-8 STS | F2-13 approvals store (00044) → F2-14 hold/release → F2-15 API + SSE | F2-18 connection tests, F2-20 credential web, F2-21 connections page | F2-22–F2-26 |
| 19 | F2-22 connections panel (after D9) | F2-23 approval page (after E13) | F2-24 agent connections card | F2-25 curated catalog + smoke, F2-26 legacy quick fix | done |

Dependency violations found in the specs' own breakdowns and fixed: A8's migration was late (split into A8a/A8b); B's 00011 sat in PR 7, behind the vault's migrations (moved to PR 3); E's backfill depended on a column B11 dropped (B11 moved to 2.3). Dependencies the specs left implicit, now in the table: D12 needs E12 (agent page frame); F2-6 needs F1-5 and B7; F2-22 needs D9; F2-23 needs E13; F1-4 after B4 (both edit `factory.schema.json` and `internal/config`); E1 after B4. (restructure) D6 no longer needs `CredentialSelect`: F1-8 later replaces D's read-only Basics lines; F1-1 (secret scope) moved from step 3 to F, so `Secrets.Resolve` keeps its signature through A–E.

## Questions answered

| Question (spec) | Answer | Source |
| --- | --- | --- |
| Factory roles ordered or separation of duties? (A1) | Ordered (editor ⊇ approver ⊇ viewer); separation of duties later | Canvas Members: one segmented choice per factory |
| Email invitations? (A3, C4) | Copy-a-link in 2.2 (`accept_url`); local mode answers 409 on accept; mail later | A §2 Out, §8.3 |
| Retention and bucket purge on org delete (A5) | 7 days, then A6's job writes delete markers on the org prefix; lifecycle rules come with phase 3 | A6; phase scope |
| Slot caps owner (A6, F1 Q1, C1) | A (A7), shown in C's Settings | Canvas Org-Settings; A7 |
| Default org name (A7) | `default`/"Default", renamed in Settings | A §14 step 5 (non-interactive `goal up`) |
| API tokens shape (A8) | Not in 2.2 (A9) | A §2 Out |
| RP-initiated logout (A9) | Not in 2.2 | No board shows sign-out from the IdP |
| Weekly budget enforcement (B1) | D §4.2 | D |
| Blob GC (B2) | Later; unreferenced blobs accumulate in 2.2 | B ADR amendment |
| Item vs run pinning (B3) | Runs pin; copy says "Runs already started finish on v8" | D §10.10 adopted the copy |
| Blank skeleton (B4) | A `blank` template in `catalog/templates/`; no names in Go | AGENTS.md rule 1 |
| F2's `connections` table vs YAML (B5, F2 Q5) | YAML is the source; `definition_versions.resolved` pins bindings; `connections` is the current projection + health, written in B's publish tx | B §5.1, §6.1; F2 §6 |
| Cross-org clone, org templates (B7) | Later | B §2 Out |
| Per-factory model access in Basics (F1 Q2) | Yes, under Advanced, once F1 adds `CredentialSelect` (read-only before F, restructure) | D §10.5 |
| Repository via GitHub connection (F1 Q3) | Keep both forms (`{url, credential}` or `{connection, repo}`; `{url, token}` with an `env:` reference until F); UI prefers the connection once F2 is wired | B §4.6 schema (oneOf) |
| Bedrock (F1 Q5) | With the AWS deployment | Phase scope |
| `GOAL_GITHUB_API_URL` enough (F1 Q6) | Yes for 2.2 | No GHES on any board |
| AWS Secrets Manager shown disabled (F1 Q7) | Disabled with its note | Canvas Org-Settings lists it |
| Tablet 768–1023 (C2) | Full sidebar per canvas, container queries | Canvas; MASTER "UI canvas is the reference" |
| Weekly budget source (C3, D5 part) | Per factory `runtime.budget.weeklyUsd`; org total = sum | B §4.6, D §4.2 |
| Old slugs (C5) | Same as A4, decision 2 | — |
| Factory marks (C6) | Derived client-side (S / Su) | Canvas marks match the derivation; no schema change |
| Rust dot for blocked items (C7) | Yes: blocked or andon | MASTER.md navigation note ("rust dot = blocked"); E §8.4 |
| Agent page ownership (D1) | E owns route and frame; D and F2 add cards | README registry |
| skills.sh search API (D2) | Start with curated `sources.yaml`; add `Search` only against a documented endpoint | D §5.1 fallback |
| Who approves inline (D6) | Inline with "Saved at once" help | Canvas New-Basics |
| Capacity history for busy share (E2) | Accept the approximation in 2.2 (display metric) | Scope |
| "1 of 3" (E3) | Top three candidate constraints | Card text is per station ("Investigate is holding the line back") |
| Live draft output (E4) | Latest agent message until the output is submitted; no `goal__draft` tool | Scope |
| Move to cancel (E7) | Not in 2.2 | Not on any board |
| Transcript retention (E8) | Moot: transcripts deferred | Rule 3 |
| Laptop OAuth (F2 Q2) | Smoke tests decide; fallback is a static client per org (`oauth` method field) | F2 §4.1 |
| Atlassian SSE vs `/v1/mcp` (F2 Q3) | Smoke test decides; SDK supports both | F2 |
| Connector updates without publish (F2 Q4) | Never: versions pin at publish; deprecate + warning | B's version model |
| Exec sidecars need coreutils (F2 Q6) | Accept for v1 (aws-cli and azure-cli ship it) | Both CLIs on the canvas qualify |
| Hosted catalog operator (F2 Q7) | Later (hosted deployment is phase 3) | Phase scope |
| goose allow-missing vs merge order (review) | Merge in number order; keep goose's default | Forward-only safety; no code change |
| `JobLocks` port (F2 needs) | Not needed: each job's store method takes `pg_try_advisory_xact_lock` | F1's `Rewrap` pattern; architecture "Singletons" |

## Decisions for the user

| # | Question | Recommendation | Alternative |
| --- | --- | --- | --- |
| 1 | In OIDC mode, who may create organizations? (A2) | Any signed-in user whose verified email is in `GOAL_OIDC_ALLOWED_DOMAINS` | Operator only (`GOAL_ORG_CREATION=operator`) |
| 2 | Renaming an org slug (A4, C5) | Accept the break, with C's warning "Links with the old address stop working" | Keep an `organization_slugs` history and redirect |
| 3 | Restore semantics (B6) | Pointer move (instant rollback, numbers never reused; history shows "Running" on the restored version) | Restore creates version N+1 with the old manifest |
| 4 | Credential rotation (F1 Q4): **moved to phase F's design pass** (credential lifecycle), not decided here (restructure) | Starting point: erase the previous ciphertext at rotation (crypto-shred; runs keep what they resolved) | Keep it 24 h with an undo endpoint |
| 5 | Prompt "Insert" buttons (D3) | Hide them in 2.2 | Add `{{item.*}}` placeholders in the worker and B's validation |
| 6 | YAML editor (D4) | Keep Monaco (ADR 0001, frontend.md) with D's phone textarea | ADR to switch to CodeMirror 6 before D8 |
| 7 | Budget and metrics time base (D5, E1) | Rolling 7 days and UTC days in 2.2; `dailyUsd` stays display-only | Calendar week in an org time zone (adds an org setting) |
| 8 | Live notes to a running agent (E5) | Ship E14a (next run) in 2.2; E14b (stdin, streaming input) only if time allows | Build both now, or drop `TaskInput` |
| 9 | Who may pull the andon cord (E6) | Any factory viewer (`andon.pull`); clearing needs approver | Approvers only |
| 10 | Held tool approvals (F2 Q1; re-examined in F's design pass, restructure) | Keep the run, container and WIP slot for up to 30 min (`approvalTimeout`) | Suspend the run and requeue on decision (worker resume + engine state) |
| 11 | Connector intake (Jira filter, GitHub issues, alert source chip): no spec owns it | Later (2.3): show intake read-only with "Edit in YAML" and hide the source chip when absent | Add an intake work package to F2 in 2.2 |

## Not resolved here

- Exact rewrites of every SQL block in B, F1, E and F2 to the tenancy kit: the specs carry a binding kit note per migration, and the guard test `TestSchema_EveryOrgTableHasRLS` enforces it. The literal SQL is fixed in the PRs.
- F1's and F2's port signatures still show explicit `org` parameters in the code blocks; the README convention and a note at each port say to drop them.
- Whether the canvas's "1 of 3" pages candidates or insights cannot be confirmed from the board's text; E's reading was kept.
