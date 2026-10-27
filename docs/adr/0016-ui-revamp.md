# ADR 0016 — The UI revamp: the canvas screens are the UI's contract

Status: accepted · 2026-10-26 · Replaces the look in [ADR 0004](0004-frontend-and-monorepo.md)'s "tokens generated from MASTER.md"; the stack in ADR 0004 is unchanged.

## Context

The owner found the first UI hard to read: muted colours, two navigation columns, and long blocks of explanatory text. They studied Warp Factories and other agent platforms and designed a new UI with Claude on the Goal UI canvas, then approved it on 2026-10-26. It has 54 screens across onboarding, daily work, intake, building a factory, Jonah, connections, the organization, the account and phones.

Agents will build it over many items. An agent that works from a summary fills the gaps with its own taste, and each gap is a place where the product drifts from the design. The owner asked that nothing drift.

## Decision

1. **The screens are the contract.** Each screen is exported into `docs/design/screens/<flow>/` as `<screen>.jpg` (the look) and `<screen>.html` (its exact layout, copy and values). [MASTER.md](../design/MASTER.md) holds the shared tokens, components and vocabulary. A screen in code matches its picture in four ways:
   - **Layout:** regions, order and proportions.
   - **Components:** the ones MASTER.md names.
   - **Copy:** the same words, with real data in place of the sample data.
   - **Colour and type:** tokens only.

   When a screen and MASTER.md disagree, MASTER.md wins and the pull request says so.
2. **Design changes go canvas-first.** Only the owner changes the design. A change is made on the canvas, then exported to `docs/design/screens/` and MASTER.md in one pull request. An implementing agent never redesigns. When a screen cannot be built as drawn (no data for it, an accessibility failure, a broken layout at 375px), the agent builds the closest version, lists each difference in its pull request, and Review sends back any difference the pull request does not explain.
3. **Review checks the match.** For each screen it touches, Review compares the change with the screen's files, and with the four states of [the web rule](../../.claude/rules/web.md): loading, empty, error and ready. Drift without an explanation is a reason to send the work back, like a failing check.
4. **Controls for features not built yet.** A screen may show a control whose feature comes later. [The screens' index](../design/screens/README.md) lists these as "Not built yet". Until the feature lands, the control renders disabled, with the tooltip "Not available yet" and `aria-disabled`. It never fakes a result. The item that builds the feature enables the control.
5. **Words change in the UI first.** The UI says Needs you, Intake, trigger, Credentials, Hands-off, Judges, Evals and Bake-offs, as MASTER.md's vocabulary has it. API, database and Go names change only when an item needs them to, so for example `inbox` stays `inbox` in the contract.
6. **The order of the work.**
   - The foundation lands first, before any screen: tokens, Archivo, the Goal mark, the sidebar shell and the phone shell.
   - Restyled screens follow, one flow per item.
   - Then the new pages over data that exists.
   - Then each new feature, end to end, with its screens: Intake triggers, Needs you questions, credentials, spend and models, onboarding, then Jonah.
   - Each item names its screens.
7. **The mock follows the screens.** `apps/web/mock` fixtures gain the data each screen shows, so the e2e tests and the mock API render the same screen the picture shows.

## Consequences

- `docs/design/screens/` adds about 10 MB of pictures and HTML. That is the price of a contract every agent can read in its box.
- The old screens' notes in `docs/engineering/frontend.md` stay true until the item that replaces each screen lands, and that item updates them.
- A design change costs one owner pull request before any code changes. That is deliberate.
