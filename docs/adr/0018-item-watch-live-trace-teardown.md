# ADR 0018 — Watch live, Trace and Teardown on an item, and OAuth consent with phase 6

Status: proposed · 2026-10-27 · Builds on [ADR 0016](0016-ui-revamp.md) (the screens are the contract, design changes go canvas-first, controls for features not built yet) and [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (runs, their boxes and what they report)

## Context

The Work item screen ([`docs/design/screens/daily-work/item.html`](../design/screens/daily-work/item.html) and its `.jpg`) draws three controls whose feature is not built: the header button Watch live (line 164) and the tabs Trace and Teardown (lines 198 and 199, beside Timeline, Spec, Changes and Checks). The [screens' index](../design/screens/README.md) lists all three as "Not built yet", and [MASTER.md](../design/MASTER.md) ("Screens") says they "are described in the item that builds them". No picture shows what any of them opens. The phone layout ([`mobile/item.html`](../design/screens/mobile/item.html)) draws none of the three.

What exists today:

- **The trace, from item events.** `apps/web/src/features/items/timeline.ts` (`buildTrace`) folds an item's events and runs into entries: each run with its tool calls and results, its agent messages, usage and cost, each check and each note. `trace-view.tsx` draws them (`TraceList`, `ToolRow`), and `item-page.tsx` shows them in a "Trace" card with a live line ("frontend-engineer working, 12 tool calls") and a "Latest steps" card. The worker reports `agent_started`, `tool_call`, `tool_result`, `agent_message` and `usage` as item events (`internal/core/domain/worker.go`, `event.go`).
- **The live path.** Every committed item event reaches the browser as `event: item` on `GET /v1/orgs/{org}/stream` (`ports.ItemNotices`, [architecture.md](../engineering/architecture.md), "Realtime stream"). `apps/web/src/shared/stream.tsx` invalidates the item's keys, and every item query (`keys.item`, `keys.events`, `keys.runs`, the parts) sits under `['org', org, 'item', item]` (`apps/web/src/api/queries.ts`), so an event refreshes them without polling. `eventsQuery` asks only for the events after the last one cached.
- **Flow data per item.** `Item.flow` carries the item's spans (station and state, in order), its target, budget and current run; `Item.touches`, `Item.cost_usd` and `Item.rework_count` sit beside it; `listItemRuns` gives each run's station, agent, model, status, turns, tokens and cost. The Time, Path and Human touches cards (`flow-cards.tsx`, `touches-card.tsx`) draw parts of this.
- **No transcripts.** Full run transcripts in the bucket were deferred by [spec 2.2-E](../history/specs/2.2-e-work-screens.md) (§2 and §4.7): AGENTS.md rule 3 lets the bucket hold only immutable content-addressed blobs (definitions, later knowledge), and a transcript carries what an agent read, which may include secrets the worker must scrub first. That spec says transcripts need an ADR before they return.
- **Teardown** is named nowhere in the specs, the glossary or the code.
- **OAuth consent.** The [consent screen](../design/screens/account/consent.html) draws "<app> wants to use Goal as <person>". The index marks it "new feature (OAuth consent, phase 6)". [Spec 6](../history/specs/6-goal-mcp-server.md) schedules it as PR 6-7, after the authorization server (6-2, migration `00070`) and the resource server (6-3, `00071`, the `/v1/mcp` mount). `apps/auth` has none of the plugins 6-2 adds.

## Decision

1. **Watch live opens the Trace tab on the running run and follows it.** A person who chooses Watch live sees the Trace tab (decision 2) scrolled to the running run's newest step, and each new tool call, result and agent message appears at the end as it is recorded; the existing live line ("<role> working, N tool calls") stays in the view's header. Scrolling up stops the following; choosing Watch live again resumes it. It is a view of the item page, not a page of its own: the URL gains the tab (`?view=trace`), so a shared link opens the same view.
   - **Data:** the item's events and runs only, as the trace reads them today. No transcript, no box output, nothing new in the bucket.
   - **API:** none new. `listItemEvents` (`GET /v1/orgs/{org}/items/{item}/events?after=`), `listItemRuns` and `getItem` (for `flow.current_run`).
   - **Live:** `event: item` on the stream invalidates `keys.item(org, item)`, which refetches `keys.events` (only the events after the last cached one) and `keys.runs`. No polling, no second stream.
   - **Disabled** when the item has no running run (`item.flow.current_run` is null, or the item is not `in_progress`). It then renders like ADR 0016's not-built controls (the `NotAvailable` helper in `apps/web/src/ui/not-available.tsx`, `aria-disabled`) and its tooltip says "No run is working on this item"; that is the one sentence of copy this ADR adds, and the owner may replace it on the canvas.
   - **CLI:** `goal follow <item>` already streams an item's events; it is the CLI's Watch live and does not change.
2. **Trace is the item's full step-by-step record, from item events.** The Trace tab shows every run of the item, oldest first, each with its tool calls and results, agent messages, checks and notes: today's "Trace" card moved into the tab. The "Latest steps" card's "Full trace" opens the tab.
   - **Data:** item events and runs (`buildTrace`). This ADR does not bring back full transcripts: a trace is what the worker reports as events, already scrubbed of secrets by the worker rule. Returning transcripts (the bucket under AGENTS.md rule 3, scrubbing, retention) needs its own ADR, and is not part of this plan.
   - **API:** none new; the same three operations as decision 1.
   - **Live:** as decision 1. A trace with a running run keeps its live line.
   - **Disabled:** never. An item with no runs shows the trace's empty state ("No tool calls yet.", the copy "Latest steps" already uses).
3. **Teardown is the after-action breakdown of one item (the proposal the owner confirms).** A Teardown is where one item's lead time, cost and human help went: each station and state it passed through with the time it spent there, each run with its agent, model, turns and cost, each send-back and failed check that made it do work again with its reason, and each human touch, set against the factory's lead-time target and the item's budget. It reads only what Goal already records (spans, runs, item events and touches), so it is computed when asked and stored nowhere. It is complete once the item is done or cancelled; on an open item it is the breakdown so far.
   - **Data:** `item_spans`, `agent_runs`, the item's events (`moved` upstream with a reason, `check_failed`, `gate_rejected`, `run_retried`) and its touches. Nothing in the bucket.
   - **Domain:** a pure `domain.TeardownOf(spans, runs, events, touches, target)` returning `domain.Teardown` in `internal/core/domain/teardown.go`, unit-tested, so the API and the CLI agree on every number.
   - **API:** one new read, `getItemTeardown`: `GET /v1/orgs/{org}/items/{item}/teardown`, `x-goal-scope: factory:read` like `getItem`, answering a `Teardown` schema (the target and the elapsed time; per station the time waiting and working; per run its cost and turns; each rework loop with its cause, its station and the event that caused it; the touches; the totals). 404 `item-not-found` as the other item reads. Named here, added by the build part.
   - **Live:** its query key sits under `keys.item(org, item)`, so the stream's item events refresh it like the others.
   - **Disabled:** never once built; an item with no history yet shows its empty state.
   - **CLI:** `goal item <item> --teardown` prints the same breakdown as text.
4. **Needs a canvas picture.** Decisions 1 and 2 are drawn with components that exist (the tabs MASTER.md draws, `SectionCard`, `TraceList`, `ToolRow`, the live line), so they need no picture. Teardown's view does not fit an existing composition: the Time, Path, Cost and Human touches cards already sit in the item page's right column, and repeating them in a tab adds nothing. So, under ADR 0016 decision 2, these wait for the owner's picture on the canvas before any UI is built:
   - the Teardown tab's ready, loading, empty and error states, at 1440, 768 and 375 px;
   - the tooltip copy for Watch live with no running run (decision 1), if the owner wants other words.

   The domain, the API operation and the CLI of decision 3 do not wait for the picture; the tab stays disabled with "Not available yet" until its view is built.
5. **OAuth consent ships with phase 6, not with this feature.** The OAuth half of `account/consent` is built by PR 6-7 of [spec 6](../history/specs/6-goal-mcp-server.md), after 6-2 (the authorization server, migration `00070`) and 6-3 (the resource server, `00071`). Built before them, a consent screen would let an app ask an authorization server that does not exist yet, or would hand out tokens that no endpoint of goald accepts: `/v1/mcp` is the only resource those tokens are for, and it comes with 6-3. The CLI sign-in half (`/device`) is a restyle and goes with the account flow's restyle item. The consent screen lists no "Not built yet" control, so nothing on it is enabled here.

## Consequences

- Watch live and Trace need no API, migration or bucket change: they are a web change over data the item page already loads. They can land as soon as the item screen's restyle has placed the tabs.
- Teardown adds one domain type, one read operation and one CLI flag, and no migration: it is computed from tables that exist. It costs a query of the item's spans, runs, events and touches per view, which the API already makes for the item page.
- The Teardown tab cannot be built until the owner draws it. Until then it renders disabled with "Not available yet", as today.
- If the owner rejects decision 3's definition of Teardown, decisions 3 and 4's Teardown parts and the follow-up rows 3 to 5 change with it; Watch live and Trace do not depend on it.
- Transcripts stay deferred. A trace shows what workers report as events, not every message a model saw.
- The glossary gets Teardown and Trace ([glossary](../reference/glossary.md)).

### The follow-up plan

Each row is one pull request. All come after the item screen's restyle (software#150 and software#153; ADR 0015), which places the header actions and the tabs.

| # | Pull request | Lane | Files it changes | Merges after |
| --- | --- | --- | --- | --- |
| 1 | Trace tab: move the Trace card into the Trace tab, `?view=trace`, "Full trace" opens it; enable Trace | frontend | `apps/web/src/features/items/item-page.tsx`, `trace-view.tsx`, their tests, `apps/web/mock` fixtures, the item e2e spec, `docs/engineering/frontend.md` | software#150, software#153 |
| 2 | Watch live: open the Trace tab on the running run and follow it; disabled with no running run; enable Watch live | frontend | `apps/web/src/features/items/item-page.tsx`, `trace-view.tsx`, `timeline.ts`, their tests, `apps/web/mock` fixtures, the item e2e spec, `docs/engineering/frontend.md` | 1 |
| 3 | Teardown in the domain and the API: `domain.Teardown`, `TeardownOf`, `getItemTeardown`, `task api:gen`, the access golden | backend | `internal/core/domain/teardown.go` and its test, `contracts/api/openapi.yaml`, `internal/api` (handler and tests, `testdata/access.golden`), generated code, `docs/engineering/architecture.md`, the glossary row's status | the owner's confirmation of decision 3 |
| 4 | Teardown in the CLI: `goal item <item> --teardown` | backend | `internal/cli` (the item command and its test), `docs/engineering/architecture.md` (the CLI line) | 3 |
| 5 | Teardown tab: the view as the owner drew it, its four states, its query under `keys.item`; enable Teardown | frontend | `apps/web/src/features/items/` (a teardown view and its tests), `apps/web/src/api/queries.ts`, `apps/web/mock` fixtures, the item e2e spec, `docs/engineering/frontend.md`, `docs/design/screens/README.md` (the "Not built yet" cell) | 3, and the owner's canvas picture (decision 4) |
| 6 | OAuth consent | per spec 6 | as PR 6-7 of [spec 6](../history/specs/6-goal-mcp-server.md) | 6-2, 6-3 |
