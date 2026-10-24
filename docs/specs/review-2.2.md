# Phase 2.2 cross-spec review

Scope: the seven specs (A–G), README conventions, AGENTS.md, architecture, testing, frontend, ADRs 0001–0006, MASTER.md and the 37 canvas boards. Every change made in a spec ends with "(review)".

## Verdict

**Buildable after the fixes below.** The seven specs now agree on names, paths, tables, events, permissions and ports, and the build order below has no cycles. The plan respects the architecture rules once the scope cuts are applied: drum-buffer-rope, the automatic andon pull and the constraint cache move to phase 4; run transcripts move out (bucket rule); multi-instance gateway forwarding moves to the AWS deployment.

Two things a builder must keep in mind:
1. **Migration PRs merge in number order.** goose (`NewProvider`, default settings) refuses a lower-numbered migration that arrives after a higher one is applied. Each phase's other PRs can run in parallel.
2. **Eleven product decisions** remain open (last section). Each has a recommended default, so building can start on those defaults.

Fixes: 48 rows in the table below, made as 182 marked edits across the README and the seven specs. By type: contracts 22, collisions 9, architecture 9, scope 6, build order 2.

## Fixes applied

| Spec | Section | Change | Why |
| --- | --- | --- | --- |
| README | Registry | Registered every new port, package, route, query-key root, stream event, NOTIFY channel, token claim, bucket prefix, migration number, shared schema; added the package/depguard table | Rule 2: one registry; specs had introduced ~40 unregistered names |
| README | Registry | Problem `code` is kebab-case; audit actions `noun.past_verb`; org from `ctx`; permissions only in A's table | B and E used kebab-case, C and G snake_case; F's audit names failed A's CHECK regex; C and G passed `org` explicitly |
| A | §5.1, §8.5, §16 | New `goal_org_events` channel and `ItemNotice.Org` for non-item stream events; the feed filters every event by org and visible factory | C's `credential` event relied on an "org-event NOTIFY" A did not have; G invented `goal_connections` |
| A | §5.1 | `api.Factories` listing moves to B's `api.Definitions` | A said "B keeps this", B removed it |
| A | §3, §8.2, §16 | `GET /settings` needs `org.read`; model access and vault live on C's `/settings/vault`; D's Save sends both requests | D shows Settings read-only to members; E needs slot caps; A and C disagreed on one vs two endpoints |
| A | §8.7 | Added `UserRef`, `Problem.code` | B needed `UserRef`; B, C, E, G need `code` |
| A | §10 | D's `subscribe(baseUrl, {org, factory?}, handlers)`; keys use the org id | A and D had different signatures; D keys by id |
| A | §11.2 | `credentials.read` → admin; added `andon.pull` (viewer), `item.move/pause/note`, `andon.clear` (approver), `factory.pause` (editor); `item.retry` → approver; `connections.bind` → `connections.test`; `factory.delete` marked later | C's list is admin-only; F's operator actions had no rows; Pause/Resume need the same role; G binds through definition edits |
| A | §6.1, §6.6 | `goal_org()` stays strict, G adds `goal_org_or_null()`; RLS test allowlist gains `vault_keys`; `connectors` named policies | G had redefined `goal_org()`; C's platform table and G's curated rows would fail the guard test |
| A | §15 | A8 split: A8a sessions migration 00007 right after A7; A8b OIDC (no migration) any time later | 00007 landing after B's 00010 breaks goose |
| B | §2, §17 | Weekly budget enforcement is E's | E took it (B Q1) |
| B | §4.7, §5.3, §16 | `authoring.Credentials` = C's `Vault.Check`; `authoring.Connectors`/`ConnectorInfo` dropped; G's checker is an `authoring.Check` + `authoring.Resolver`; `Members` = A's `Tenancy.Members` | B needed `CredentialStates` and `Connectors(refs)`, which C and G don't provide; A has no `Approvers` |
| B | §5.1, §6.1 | `Publish.Resolved`, `Publish.Bindings`; `definition_versions.resolved jsonb` | Resolves B Q5 and G Q5: runs on older versions read their pinned bindings; B's tx projects `connections` |
| B | §6.1 | A's tenancy kit (`DEFAULT goal_org()`, `ON DELETE CASCADE`, `org_isolation` with `(SELECT goal_org())`) | Rule: RLS + org_id per A; purge needs cascade |
| B | §6.1, §14, §15 | `00012` (drop `factories.definition`) moved to 2.3; `00011` moved into PR 3 | F's 00030 backfill reads that column and runs after 00012 on a fresh DB; migration order |
| B | §7, §14 | Legacy import reads A's interim `orgs/<org>/factories/<id>/`, not `factories/<id>/` | A4 moves and deletes the old keys before B runs |
| B | §8.1, §8.3, §16 | `GET /factories` is one operation returning A's `Factory` + `current/draft/paused`; `FactorySummary` gone; roles mapped to permissions | Path defined twice (A, B); `FactorySummary` collided with D's schema |
| B | §8.2 | `Validation.problems` typed `DefinitionProblem`; FactoryModel gains `repository.credential`, `model_credential`, `skills[].origin`, `flow.targets` | E §16 asked for them (C's, E's, F's fields) |
| B | §8.4, §16, §18 | Stream event union owned by D, feed by A; ADR amendment lists all three catalogs | Owner was "F/D"; rule 3 needed the catalogs named |
| C | §5.2 | Org from `ctx`, explicit `org` params dropped | README convention (A) |
| C | §6.1, §6.2 | Tenancy kit, `org_isolation` names, cascade, composite `credential_uses.run_id` | A's guard test requires `org_isolation`; purge |
| C | §6.6 | `credentials.connector`/`method` text (not `connector_id`), `needs_reconnect`, sealing inside the postgres adapter, `Resolve`/`Rotate` mapping for G | G's needs (`AddVersion`, `SetStatus`, `MarkUsed`, `Seal/Open`) mapped onto C's port without new core methods except `MarkNeedsReconnect` |
| C | §8 | Roles → permissions; kebab codes; `GET /settings/vault` is `org.read`; SSE via `goal_org_events` | D's read-only settings; channel gap |
| C | §10.1 | Route files under `o.$org._org` | D's org-sidebar layout |
| D | §2, §8, §10, §15, §16 | `/factories/summary` → `/overview` (`FactoryOverview[List]`); `/waiting` → `/inbox` shared with F (`InboxSource` registry in D2) | `summary` shadowed a factory named "summary"; D and F each built an inbox |
| D | §4, §5 | One `domain.AndonPull` (F extends); `Store.WaitingHuman` interim until F's `InboxItems` | Duplicate type; same postgres type implements both ports |
| D | §8 | `budget_usd` from `weeklyUsd` | B/E schema (D Q3) |
| D | §10.8, §10.9, §16 | Members and invitations separate queries; settings PATCH on `/settings` + C's vault endpoint; slots from A7 | A's actual endpoints |
| D | §10.10 | `invalidationsFor` names work keys one by one; never the bare factory prefix | Item bursts would refetch B's drafts and C's factory credentials (E's finding) |
| D | §10.1, §16 | `/agents/:agent` owned by F; Credentials pages by C | Two specs claimed the route; "C or G" |
| D | §10.11 | New tokens go into MASTER.md first | Rule 10; tokens are generated from MASTER.md |
| E | §10.6 | DBR queue sentence and Flow panel say "enforced from phase 4" | Scope cut in F |
| E | §5.2 | `api` rule denies `internal/builder` | Boundary listed for every new package |
| E | §16 | Query-key issue resolved; slots via `GET /settings`; `builtins: ask` stays a warning | Fixed in D/F; A change; G gates gateway tools only |
| F | §1, §2 | Deferred list: DBR release, expedite limit, automatic rework pull, constraint cache, transcripts | Roadmap phase 4; rule 3 |
| F | §4.3, §4.4, §4.7, §7 | Marked deferred; manual andon still stops the line | Canvas needs "Pull the andon cord" to act |
| F | §4.5 | `by domain.Actor` on every engine method | A §4.2 |
| F | §5.1, §5.2 | `FlowStats.Attention` → `InboxItems`; `ReleaseUpTo`, `SetRunTranscript` deferred | Method-name clash with D's `Store.Attention` on one Go type |
| F | §6 | Tenancy kit, composite parent keys; backfill tolerates NULL `factories.definition` | Rule; B stops writing that column at B7 |
| F | §8 | Roles → permissions; `Board.line.release.enforced`, `budget`; `ItemCard.pending_approval`; `/inbox` is D's without `factories[]`; G's `ToolApproval`; no `by` | Contracts with A, D, E, G |
| F | §10.3, §11 | Explicit invalidation keys; audit names `andon.pulled`, … ; no transcript read audit | Collision; A's CHECK regex |
| F | §13, §15 | F8 display only, F9 manual andon, F11 extends D's inbox, F13 runs without transcripts, F14 split a/b; deferred tests noted | Scope |
| G | §1, §2, §4.5, §4.7, §5.3, §14, §15 | Forwarding (`g` claim, `GOAL_PEER_URL`, G16) deferred to the AWS deployment | One goald in 2.2; no board needs it |
| G | §4.3 | Checker = `authoring.Check` + `Resolver`; kebab codes; three codes merged with B's; `blocked` severity → warning | B's pipeline shape and severities |
| G | §5.1, §5.5 | Org from `ctx`; sealing in the adapter; `connectors` may import `authoring`; `api` denies `connectors` | Conventions, depguard |
| G | §6 | `goal_org_or_null()` only for `connectors`; tenancy kit elsewhere; adds C's credential columns; `connection` event on `goal_org_events` | A's strict helper; channel registry |
| G | §8, §10 | Roles → permissions; query keys under `['org', id]` | A's table; D's root |
| G | §16 | Needs from A, B, C, D/F rewritten to what those specs provide; no `JobLocks` port | Contract mismatches |

## Contract matrix

| Provider → consumer | Interface | Status |
| --- | --- | --- |
| A → all | `domain.OrgID/UserID/Actor`, `WithOrg/OrgFrom`, `ErrNoOrg`, tenancy SQL kit, `goal_owner`/`goal_app` | ok |
| A → all | Permission table (A §11.2), `x-goal-permission`, `s.factory/s.item` resolvers | fixed (rows for B, C, F, G) |
| A → B, C, F, G | `Tenancy.ActiveOrgs` for cross-org jobs (refresh, rewrap, sweeps) | ok (answers C and G needs) |
| A → B | `Tenancy.Members` for station approvers | fixed (was `Approvers`) |
| A → D | `/me`, `/orgs`, `/orgs/{org}`, `/settings`, `/members`, `/invitations`, `/transfer`, DELETE org | fixed (D's paths and shapes) |
| A → E | `GET /settings` slots readable by members | fixed |
| A ↔ D | `subscribe(baseUrl, {org, factory?}, handlers)` | fixed |
| A → all | `Problem.code` (kebab-case), `UserRef` | fixed |
| A → C, G | `goal_org_events` NOTIFY, `ItemNotice.Org` | fixed (new) |
| A → G | `goal_org()` strict; G's own `goal_org_or_null()` | fixed |
| A, B → G | Task token claims `o`, `v` | ok (`g` deferred) |
| B → A, D, E | `GET /factories` → `Factory` (+`current`, `draft`, `paused`) | fixed (collision) |
| B → E | Draft/model/files/validate/publish/versions/compare/templates API, `defKeys`, `FactoryModel` | fixed (model additions) |
| B → D, E, F | `definition` stream event from `goal_definitions` | fixed (owner) |
| B → F | `agent_runs.definition_version_id`, `moved` with `reason`, `paused` | ok |
| B → G | `definition_versions.resolved`, `Publish.Bindings` → `connections` rows in the publish tx | fixed (new) |
| B → C | Draft/publish to rewrite references at cutover | ok (C9 after B10) |
| C → B | `Vault.Check(ctx, factory, uses)` + `config.Factory.CredentialUses()` | fixed (was `CredentialStates`) |
| C → D | `/settings/vault`; `credentials_needing_attention` from `Vault.List` statuses (admins; 0 for others) | fixed |
| C → E | `CredentialSelect`, `GET /factories/{f}/credentials`, `credential` event | ok |
| C → G | `Vault.Resolve`/`Rotate`/`Credential`, `NewCredential.Connector/Method`, `MarkNeedsReconnect`, `envelope.go` in-adapter | fixed (mapped) |
| D → B, C, E, F, G | Route tree, `screens.ts`, `streamHandlers`, `invalidationsFor`, UI inventory | fixed (explicit keys) |
| D → F, G | `/attention`, `/inbox` + `InboxSource` registry, `domain.AndonPull` | fixed (merged with F's inbox) |
| D → B, F, G | `/overview` (`FactoryOverview`) | fixed (renamed) |
| E → B, F, D | Weekly budget enforcement, `Store.SpendSince`, `Board.line.budget` | ok (F's Board field added) |
| E → F, G | Agent page cards; `shared/connections-editor` slot; `/definition/*` deep links | ok |
| F → D | Inbox kinds, `FlowStats.InboxItems`, extended `AndonPull`, `event: factory` | fixed |
| F → E | `flow.targets`, Agent page frame and route | ok (route ownership fixed) |
| F → G | `ItemCard.pending_approval`, item timeline | fixed (accepted) |
| G → B | `connectors.Checker` as `authoring.Check`/`Resolver`, problem codes | fixed |
| G → D, F | `tool_approval` `InboxSource`, `/approvals`, `approval`/`connection` events, `connections_blocked` | fixed |
| G → E | `shared/connections-editor`, capability flags, template "Connects to" | ok |

## Canvas coverage

| Board | Owner | Elements owned elsewhere or later |
| --- | --- | --- |
| Main (Floor) | F | Rail and sidebar D; "Add an investigator" target E; Jira comment entry G; "Knowledge sync failed" later (phase 3); Jonah badge later; rework warning is display only (auto pull phase 4) |
| Board | F | Rope sentence display only (DBR phase 4); expedite limit display only; chrome D; Connections dot G |
| Item | F | "From a Grafana alert" chip: connector intake, **unowned → later** (decision 11); Knowledge attached later; Draft diagnosis = latest agent message; Note F14a/F14b |
| Agent | F (frame, stats, reasons) | Skills and Tool policy cards, Edit in Definition E; Connections card G; "See the proposed skill change" later (Jonah) |
| Connections | G | Sidebar dot D |
| Knowledge, Mobile-Knowledge | later (phase 3) | D shows the entry as "Later" |
| Jonah, Remediation, Mobile-Jonah | later (Jonah) | D shows the entry as "Later" |
| Factories, Mobile-Factories | D | Version line B; busy share F; tool-approval rows G; New factory and Import targets E; weekly budget E |
| Org-Switcher, Mobile-Org-Switcher | D | Data A |
| Members | D | Endpoints A; role copy static |
| Org-Settings | D | General, slots, transfer, delete A; Model access and Vault C; vault move later (only PostgreSQL in 2.2) |
| Catalog | G | Chrome D |
| Credentials | C | Connector columns, OAuth/App/STS rows G |
| Credential-New | C (step 3, save) | Steps 1–2, OAuth, connector fields G |
| Approval | G | Item timeline F |
| New-Start | E | Templates and creation B; "Connects to" B/G |
| New-Basics, Define-Basics | E | Credentials C; GitHub-connection picker G; Who approves A |
| New-Line, Define (Line) | E | Line checks B; constraint/busy F; "Jira filter" and "GitHub issues" intake later (decision 11), read-only; "pnpm test adds about 2 minutes" later (needs check timings); DBR queue note "from phase 4" |
| New-Agents, Define-Agents | E | "Ask me" built-in tools warn until built-in approvals exist (later); "Insert" hidden (decision 5); "Open agent page" F |
| New-Connections, Define-Connections | G (panel) | Section shell E; problems B/G |
| New-Review, Define-Publish | E | Checks/compare/publish B; slots A; G checks; dry-run checkbox later (Jonah) |
| Define-YAML | E | Ranges, problems, format B; connection rules G |
| Mobile-Floor, Mobile-Board, Mobile-Item, Phone | F | Phone chrome D; "Jonah proposes a fix" later |
| Mobile-Agent | F (stats) | Cards E/G |
| Shell on every board (rail, sidebars, tab bar, sheets, tooltip) | D | Counts from D's `/attention` (C, F, G fill parts) |

## Scope decisions

| Item | Spec | Decision | Reason |
| --- | --- | --- | --- |
| Drum-buffer-rope release (`ReleaseUpTo`, rope) | F | Deferred to phase 4 | Roadmap phase 4; the Board only needs the configured policy and counts |
| Expedite-lane WIP enforcement | F | Deferred to phase 4 | Part of release; "1 of 1 in use" is a count |
| Automatic rework-rate andon pull | F | Deferred to phase 4 | Roadmap andon; the board shows a warning and a manual cord |
| Manual andon pull/clear stopping the line | F | Kept | Canvas button must act |
| Detected-constraint cache feeding the engine | F | Deferred to phase 4 | Only DBR uses it; the Floor's constraint report stays a read model |
| Constraint report, lead-time bridge, busy share, `WaitSaved` estimate | F | Kept | Main board shows them (display) |
| Run transcripts in the bucket | F | Deferred; needs an ADR | AGENTS.md rule 3; no board shows a full transcript |
| Live notes over stdin (F14b) | F | Kept as a separable PR | The canvas shows notes to a running agent; F14a (next run) gives the behaviour without the worker change (decision 8) |
| Weekly budget enforcement | E | Kept | New-Review/Define-Publish expose drain/stop; a setting that does nothing would mislead |
| Org and per-factory slot caps | A | Kept | Org-Settings shows them ("factories wait for each other at peak") |
| Gateway multi-instance forwarding | G | Deferred to the AWS deployment | One goald in 2.2; architecture.md already documents the constraint |
| MCP Registry import, OAuth, CLI sidecars, GitHub App and STS minting | G | Kept | Catalog, Credential-New, Credentials and Connections boards show them |
| OIDC sign-in | A | Kept (A8b, last) | README decision; local mode unaffected |
| API tokens (A9), KMS adapter, Bedrock, Secrets Manager, vault move | A, C | Out of 2.2 | Not on boards / need the AWS deployment |
| Connector intake (Jira filter, GitHub issues, alert source) | none | Later (decision 11) | No spec owns it; it is new engine work |
| Drop of `factories.definition` | B | Moved to 2.3 | F's backfill depends on it |

## Architecture findings

| Rule | Spec | Status |
| --- | --- | --- |
| RLS + `org_id` on every org table, A's kit | B, C, F, G | Fixed: B, C, F and G used `current_setting` directly, other policy names, no `DEFAULT goal_org()`, no cascade, single-column FKs |
| `goal_org()` fails closed when unset | G | Fixed: G's tolerant version renamed and confined to `connectors` |
| Hexagonal: engine and domain import only ports | all | OK; recommend turning the engine depguard rule into an allow-list (README) |
| api imports no adapters or core services directly | B, C, E, G | Fixed: `api` denies `authoring`, `factorymodel`, `secrets`, `builder`, `connectors` |
| New ports in `internal/ports`, wiring only in `cmd/*` | all | OK (`SkillSource`, `FlowStats`, `Minter`, `MCPRegistry`, … registered) |
| depguard rule for every new package | B, C, E, G | OK after fixes (README table) |
| Port methods take the org from `ctx` | C, G | Fixed (convention; explicit `org` params dropped) |
| Stateless goald | E, B (caches), F (`Interrupt` map of own runs), G (sidecars, held approvals) | OK for one instance: caches are recomputable, and F's map is the cancel handle of runs it owns (heartbeat fallback). G's per-instance state is accepted only because forwarding is deferred, as architecture.md documents |
| PostgreSQL only; bucket only for definitions | F | Fixed: transcripts deferred. Catalogs (`catalog/templates`, `skills`, `connectors`) are definition content; the ADR 0005 amendment says so |
| No factory data in code or env | B (blank skeleton), C (kinds), E (`GOAL_MODELS`) | OK: blank comes from a catalog template; credential kinds and the model list are platform settings |
| Forward-only migrations | B, F | Fixed: B's drop moved after F's backfill; merge order rule (build sequence) |
| Composite FKs so a row cannot point across orgs | B, C, F, G | Fixed in text (kit note); B's `agent_runs.definition_version_id` stays single-column, RLS covers it |
| Audit action format | F | Fixed |
| UI per MASTER.md | D | Fixed: new tokens go into MASTER.md first. MASTER's "rust dot = blocked" settles D Q7 |
| Tests protect behaviour | all | OK; F's tests for deferred features move with them |
| No employer names | all | OK (only `acme`, `example.com` and the canvas's own names) |
| Terraform / knowledge base before phase 3 | all | OK (KMS, IAM and KB stay designed or out) |

## Build sequence

Rule: a PR with a migration merges only after every lower-numbered migration has merged: 00004 A2 → 00005 A3 → 00006 A7 → 00007 A8a → 00010–11 B3 → 00020–21 C3 → 00030 F2 → 00031 F3 → 00032 F9 → 00033 F11 → 00040 G2 → 00041 G11 → 00042 G13. PRs without migrations follow only their own dependencies.

| Step | Lane 1 | Lane 2 | Lane 3 | Lane 4 | Blocks |
| --- | --- | --- | --- | --- | --- |
| 1 | A1 roles | D1 api-client (on today's routes) | B4 config problems + schema, B5 factorymodel, B6 diff (pure) | B1 ADR amendment | — |
| 2 | A2 tenancy (00004) | | | | A3 |
| 3 | A3 org scope (00005) | B2 ports + fakes | C1 secret scope | | A4, B3, C2 |
| 4 | A4 org-aware engine + bucket | C2 vault domain, keyfile, envelope | C4 credential schema (after B4) | F1 flow targets (after B4) | A5, B7 |
| 5 | A5 routes + authz | | | E2 purpose (after B4) | everything with API or web |
| 6 | A6 members/settings | D2 shell API (`/attention`, `/overview`, `/inbox`) | D3 org routes + shell | B8 authoring | A7, D4–D7, B9 |
| 7 | A7 slots (00006) → A8a sessions (00007) | D4 org menu, D5 factories home | B9 definitions API | E3 skills catalog | B3 (migration order) |
| 8 | B3 definitions store (00010–11) | D6 members | A8b OIDC (any time from here) | | B7, C3 |
| 9 | B7 engine pins versions + legacy import | C3 postgres vault (00020–21) | B10 CLI on the API | | C5, F2, E1 |
| 10 | C5 resolution | C6 vault API + probes | E1 weekly budget | | C7, C8, D7 |
| 11 | C7 CLI + import-env | C8 web credentials | D7 org settings | E4 draft session, E5 mock | E6 |
| 12 | F2 item spans (00030) → F3 lease spans (00031) | E6 frame + Basics (after C8, D3) | C9 cutover (after B10, C7) | | F4–F12, E7–E11 |
| 13 | F4 metrics, F5 bridge, F6 constraint report, F7 board stats | E7 line, E8 agents, E9 connections shell, E10 YAML, E11 review | F10 move/pause/send back, F12 agent stats, F13 runs, F14a notes | F9 manual andon (00032) | F8, F11, E12, E13 |
| 14 | F8 line display | F11 inbox kinds (00033, after F9, F10, D2) | E12 agent cards (after F12), E13 wizard | F14b live notes (optional) | G2 |
| 15 | G1 connector package (may start earlier) → G2 catalog (00040) → G3 sync, G4 catalog API | G10 OAuth adapter | G17 registry | | G5, G11 |
| 16 | G5 connector connections + B publish hook → G6 gateway | G11 OAuth service (00041) → G12 credential tests | G9 exec sidecars | G19 web catalog | G7, G13 |
| 17 | G7 GitHub App minting, G8 STS | G13 approvals store (00042) → G14 hold/release → G15 API + SSE | G18 connection tests | G20 credential web, G21 connections page | G22–G26 |
| 18 | G22 connections panel (after E9) | G23 approval page (after F13) | G24 agent connections card | G25 curated catalog + smoke, G26 legacy quick fix | done |

Dependency violations found in the specs' own breakdowns and fixed: A8's migration was late (split into A8a/A8b); B's 00011 sat in PR 7, behind C3 (moved to PR 3); F's backfill depended on a column B11 dropped (B11 moved to 2.3). Dependencies the specs left implicit, now in the table: E6 needs C8 (`CredentialSelect`); E12 needs F12 (agent page frame); G6 needs C5 and B7; G22 needs E9; G23 needs F13; C4 after B4 (both edit `factory.schema.json` and `internal/config`); F1 after B4.

## Questions answered

| Question (spec) | Answer | Source |
| --- | --- | --- |
| Factory roles ordered or separation of duties? (A1) | Ordered (editor ⊇ approver ⊇ viewer); separation of duties later | Canvas Members: one segmented choice per factory |
| Email invitations? (A3, D4) | Copy-a-link in 2.2 (`accept_url`); local mode answers 409 on accept; mail later | A §2 Out, §8.3 |
| Retention and bucket purge on org delete (A5) | 7 days, then A6's job writes delete markers on the org prefix; lifecycle rules come with phase 3 | A6; phase scope |
| Slot caps owner (A6, C1, D1) | A (A7), shown in D's Settings | Canvas Org-Settings; A7 |
| Default org name (A7) | `default`/"Default", renamed in Settings | A §14 step 5 (non-interactive `goal up`) |
| API tokens shape (A8) | Not in 2.2 (A9) | A §2 Out |
| RP-initiated logout (A9) | Not in 2.2 | No board shows sign-out from the IdP |
| Weekly budget enforcement (B1) | E §4.2 | E |
| Blob GC (B2) | Later; unreferenced blobs accumulate in 2.2 | B ADR amendment |
| Item vs run pinning (B3) | Runs pin; copy says "Runs already started finish on v8" | E §10.10 adopted the copy |
| Blank skeleton (B4) | A `blank` template in `catalog/templates/`; no names in Go | AGENTS.md rule 1 |
| G's `connections` table vs YAML (B5, G5) | YAML is the source; `definition_versions.resolved` pins bindings; `connections` is the current projection + health, written in B's publish tx | B §5.1, §6.1; G §6 |
| Cross-org clone, org templates (B7) | Later | B §2 Out |
| Per-factory model access in Basics (C2) | Yes, under Advanced | E §10.5 |
| Repository via GitHub connection (C3) | Keep both forms (`{url, credential}` or `{connection, repo}`); UI prefers the connection once G is wired | B §4.6 schema (oneOf) |
| Bedrock (C5) | With the AWS deployment | Phase scope |
| `GOAL_GITHUB_API_URL` enough (C6) | Yes for 2.2 | No GHES on any board |
| AWS Secrets Manager shown disabled (C7) | Disabled with its note | Canvas Org-Settings lists it |
| Tablet 768–1023 (D2) | Full sidebar per canvas, container queries | Canvas; MASTER "UI canvas is the reference" |
| Weekly budget source (D3, E5 part) | Per factory `runtime.budget.weeklyUsd`; org total = sum | B §4.6, E §4.2 |
| Old slugs (D5) | Same as A4, decision 2 | — |
| Factory marks (D6) | Derived client-side (S / Su) | Canvas marks match the derivation; no schema change |
| Rust dot for blocked items (D7) | Yes: blocked or andon | MASTER.md navigation note ("rust dot = blocked"); F §8.4 |
| Agent page ownership (E1) | F owns route and frame; E and G add cards | README registry |
| skills.sh search API (E2) | Start with curated `sources.yaml`; add `Search` only against a documented endpoint | E §5.1 fallback |
| Who approves inline (E6) | Inline with "Saved at once" help | Canvas New-Basics |
| Capacity history for busy share (F2) | Accept the approximation in 2.2 (display metric) | Scope |
| "1 of 3" (F3) | Top three candidate constraints | Card text is per station ("Investigate is holding the line back") |
| Live draft output (F4) | Latest agent message until the output is submitted; no `goal__draft` tool | Scope |
| Move to cancel (F7) | Not in 2.2 | Not on any board |
| Transcript retention (F8) | Moot: transcripts deferred | Rule 3 |
| Laptop OAuth (G2) | Smoke tests decide; fallback is a static client per org (`oauth` method field) | G §4.1 |
| Atlassian SSE vs `/v1/mcp` (G3) | Smoke test decides; SDK supports both | G |
| Connector updates without publish (G4) | Never: versions pin at publish; deprecate + warning | B's version model |
| Exec sidecars need coreutils (G6) | Accept for v1 (aws-cli and azure-cli ship it) | Both CLIs on the canvas qualify |
| Hosted catalog operator (G7) | Later (hosted deployment is phase 3) | Phase scope |
| goose allow-missing vs merge order (review) | Merge in number order; keep goose's default | Forward-only safety; no code change |
| `JobLocks` port (G needs) | Not needed: each job's store method takes `pg_try_advisory_xact_lock` | C's `Rewrap` pattern; architecture "Singletons" |

## Decisions for the user

| # | Question | Recommendation | Alternative |
| --- | --- | --- | --- |
| 1 | In OIDC mode, who may create organizations? (A2) | Any signed-in user whose verified email is in `GOAL_OIDC_ALLOWED_DOMAINS` | Operator only (`GOAL_ORG_CREATION=operator`) |
| 2 | Renaming an org slug (A4, D5) | Accept the break, with D's warning "Links with the old address stop working" | Keep an `organization_slugs` history and redirect |
| 3 | Restore semantics (B6) | Pointer move (instant rollback, numbers never reused; history shows "Running" on the restored version) | Restore creates version N+1 with the old manifest |
| 4 | Credential rotation (C4) | Erase the previous ciphertext at rotation (crypto-shred; runs keep what they resolved) | Keep it 24 h with an undo endpoint |
| 5 | Prompt "Insert" buttons (E3) | Hide them in 2.2 | Add `{{item.*}}` placeholders in the worker and B's validation |
| 6 | YAML editor (E4) | Keep Monaco (ADR 0001, frontend.md) with E's phone textarea | ADR to switch to CodeMirror 6 before E8 |
| 7 | Budget and metrics time base (E5, F1) | Rolling 7 days and UTC days in 2.2; `dailyUsd` stays display-only | Calendar week in an org time zone (adds an org setting) |
| 8 | Live notes to a running agent (F5) | Ship F14a (next run) in 2.2; F14b (stdin, streaming input) only if time allows | Build both now, or drop `TaskInput` |
| 9 | Who may pull the andon cord (F6) | Any factory viewer (`andon.pull`); clearing needs approver | Approvers only |
| 10 | Held tool approvals (G1) | Keep the run, container and WIP slot for up to 30 min (`approvalTimeout`) | Suspend the run and requeue on decision (worker resume + engine state) |
| 11 | Connector intake (Jira filter, GitHub issues, alert source chip): no spec owns it | Later (2.3): show intake read-only with "Edit in YAML" and hide the source chip when absent | Add an intake work package to G in 2.2 |

## Not resolved here

- Exact rewrites of every SQL block in B, C, F and G to the tenancy kit: the specs carry a binding kit note per migration, and the guard test `TestSchema_EveryOrgTableHasRLS` enforces it. The literal SQL is fixed in the PRs.
- C's and G's port signatures still show explicit `org` parameters in the code blocks; the README convention and a note at each port say to drop them.
- Whether the canvas's "1 of 3" pages candidates or insights cannot be confirmed from the board's text; F's reading was kept.
