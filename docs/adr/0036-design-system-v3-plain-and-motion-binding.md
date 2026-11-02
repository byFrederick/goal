# ADR 0036 — Design system v3 "Plain" and MOTION.md are binding

Status: accepted · 2026-11-02 · Replaces the look in [ADR 0016](0016-ui-revamp.md) (its decisions 1 and 4 stay) · Builds on [ADR 0016](0016-ui-revamp.md) (the screens are the contract)

## Context

ADR 0016 set the UI's look from the 2026-10-26 revamp: cobalt and violet, Archivo for text, and a navy "andon" panel for the line. On 2026-10-26 the owner approved that revamp, but it still read as generic AI product UI. Its dark panel sat inside light pages, and its colours and type were the default most AI tools pick.

On 2026-11-01 the owner approved design system v3 "Plain". Its sources are [MASTER.md](../design/MASTER.md) (version 3), [MOTION.md](../design/MOTION.md) (the motion rules, tokens and catalogue) and the live audit of 2026-11-01 ([live-audit-2026-11-01.md](../design/live-audit-2026-11-01.md)). The screens in [docs/design/screens/](../design/screens/README.md) are exported from the owner's canvas for v3. Each one is a picture and its HTML, as ADR 0016 required.

Three things change for the people who build the UI:

- **The look.** Neutral white and near-black, grey rules, one blue for running, orange for waiting on you, red for rework, yellow for the constraint and green for done. Fonts are IBM Plex Sans and IBM Plex Mono. The andon panel is now a plain section on the page.
- **The motion.** MOTION.md says what moves, how long and with which easing. Every element that appears, disappears, resizes or changes state has a catalogue row.
- **The tokens.** `apps/web/src/styles.css` holds v3's values, and the motion tokens sit beside them.

## Decision

1. **Design system v3 "Plain" is the UI's look.** [MASTER.md](../design/MASTER.md) version 3 and its screens in `docs/design/screens/` are binding. They replace ADR 0016's look: the palette, the type, the radii, the shell and the component rules that ADR 0016 took from version 2. Where MASTER.md and a screen disagree, MASTER.md wins, as ADR 0016 already said. The sources are committed byte for byte from the design branch. Only the owner changes them (ADR 0016, decision 2).
2. **MOTION.md is binding.** Every UI change follows [MOTION.md](../design/MOTION.md). A change that adds an element which appears, disappears, resizes or changes state without its catalogue transition is rejected, and so is motion the file does not allow. A change that needs a transition the catalogue lacks adds its row to MOTION.md in the same pull request and says why. Its tokens live under `@theme` in `apps/web/src/styles.css`, and no component names a raw duration, `cubic-bezier()` or `transition: all`. `apps/web/src/ui/motion.test.ts` enforces that.
3. **Tokens keep their names across themes, and the web app uses token names only.** Light and dark values come from MASTER.md's colour table. `--color-action` and `--color-action-ink` are the primary button's fill and text. The andon tokens, `--color-run-soft`, `--radius-andon`, `--tracking-numeral` and the `font-numeral` utility are removed. Their uses move to the nearest v3 token. `--color-card` stays as an alias of `--color-shell`, because MASTER.md has no card token.
4. **Controls without a backend say "Not available yet".** ADR 0016's decision 4 stays. A control whose feature is not built renders disabled, with the words "Not available yet". No screen shows a control that does nothing.
5. **The screens stay the contract.** ADR 0016's decision 1 stays: each screen in `docs/design/screens/` is the contract for layout, components, copy and colour, and a difference is listed in the pull request.
6. **Fonts are IBM Plex**, self-hosted through `@fontsource/ibm-plex-sans` (weights 400, 500, 600 and italic 400) and `@fontsource/ibm-plex-mono` (400 and 500). Archivo and JetBrains Mono are removed.
7. **Loading.** MOTION.md's Loading row shows a skeleton of the real layout while a route or panel waits on data, which conflicts with the Loading bullet in [.claude/rules/web.md](../../.claude/rules/web.md) ("never a gray placeholder shape"). This ADR does not change the bullet. The part that adds the `Skeleton` component (the routes-docs and controls work in assembly software#365) changes the bullet, so skeletons and the bullet agree in the same pull request.

## Consequences

- The web app's look changes all at once. Screens look partly restyled until the restyle items land, because they now use v3 tokens before their components are rebuilt.
- Dark panels inside light pages (the andon panel, the onboarding preview) are plain sections now. Their text colour is the nearest v3 text token, not white.
- A reviewer checks two documents per UI change: MASTER.md (and the screen it draws) and MOTION.md (the catalogue rows the change touches).
- The motion tokens are the only durations in the app. Tailwind's `duration-*` and `ease-*` utilities read the same values, so a new component uses them without writing a number.
- Reduced motion keeps every state change and drops only movement. Transforms and blur do not transition, opacity fades run at `--duration-quick`, and the running dot and spinners stop.
- ADR 0016 keeps its record. Its look sections (type, colour, radii and the andon panel) describe the 2026-10-26 revamp and are superseded by this ADR. Read them as history.
