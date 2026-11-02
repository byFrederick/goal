# Goal motion

How Goal moves. Every UI change follows this file: a new screen, a new component, or a change to an existing one. The reviewer rejects a change that adds an element which appears, disappears, resizes or changes state without the transition listed here, and one that adds motion this file does not allow.

Sources: the [transitions.dev](https://transitions.dev) token scale and recipes (the `transitions-dev` skill), and Emil Kowalski's design-engineering rules (the `emil-design-eng` and `review-animations` skills). Load them before any motion work.

## Why

Without transitions every click lands in a single frame, so the UI feels abrupt and you lose track of where things went. Motion in Goal has four jobs only:
1. **Feedback**: the interface heard you (a button press, a toggle).
2. **Continuity**: where something came from and went (a menu from its trigger, a card moving to the next column, a list ↔ detail).
3. **State change**: a value or status changed in place (a count, "Running" → "Done").
4. **Softening**: nothing pops in or out of existence.

Decoration is not a job. If a motion does none of these, remove it.

## Rules

1. **Frequency decides.** Things done hundreds of times a day get no animation, or the lightest one: ⌘K search opens instantly; keyboard navigation in lists, keyboard shortcuts and typing never animate. Hover is a colour change only. Occasional surfaces (menus, dialogs, sheets, toasts) get the standard transition. Rare moments (onboarding steps, the first item merging) may take a little more.
2. **Fast.** UI transitions stay at or under 250ms; only panels, sheets and toasts reach 350–400ms. Exits are faster than entrances.
3. **Ease out on enter, never ease-in.** Use `--ease-out` for anything entering, opening or moving; `ease` for colour; `linear` only for constant motion (spinners, progress, shimmer).
4. **Transform and opacity only.** Animate `transform`, `opacity` and, sparingly, `filter: blur()` of at most 3px. Never animate `width`, `height`, `top`, `left`, `margin` or `box-shadow` on large surfaces; use `grid-template-rows: 0fr → 1fr` for height and FLIP or the View Transitions API for layout moves.
5. **Nothing appears from nothing.** Start from `scale(0.96–0.98)` and `opacity: 0`, never `scale(0)`.
6. **Origin-aware.** Menus, popovers and tooltips grow from their trigger (`transform-origin` from Radix's `--radix-*-transform-origin`). Dialogs grow from the centre.
7. **Interruptible.** Use CSS transitions driven by `data-state`, not keyframes, for anything that can be reversed mid-way (menus, accordions, toggles, the sidebar), so a quick reopen continues from where it is.
8. **Name every value with a token.** No raw `ms`, `cubic-bezier()` or `transition: all` in components. `apps/web/src/ui/motion.test.ts` fails on them, as `raw-colors.test.ts` does for colour.
9. **Reduced motion.** Under `prefers-reduced-motion: reduce`, drop every transform and blur and keep opacity fades at `--duration-quick`; spinners and the running dot stay still. Never remove a state change itself, only its movement.
10. **One ambient motion.** The running dot pulses (2s, ease-in-out). Nothing else moves without a cause.

## Tokens

They live in `apps/web/src/styles.css` under `@theme` and are documented here. The values are transitions.dev's scale, with Emil Kowalski's stronger ease-out.

| Token | Value | Use |
|---|---|---|
| `--duration-stagger` | 40ms | per-item offset in a list entering (cap at 5 items) |
| `--duration-micro` | 80ms | tooltip delay, press feedback, error shake segment |
| `--duration-quick` | 150ms | menu and dialog close, tooltip open, text swap, hover colour, route fade |
| `--duration-fast` | 250ms | menu and dialog open, toast close, tabs pill, icon swap, card move, page slide, accordion |
| `--duration-medium` | 350ms | sheet close, toast open, toggle thumb |
| `--duration-slow` | 400ms | sheet open, skeleton → content reveal |
| `--duration-emphasis` | 500ms | rare: a success check, a number pop-in on a KPI |
| `--duration-pulse` | 1s | one skeleton pulse, linear (`Skeleton`) |
| `--duration-shimmer` | 2s | one sweep of an agent's working status line, linear, looping (`thinking` utility) |
| `--ease-out` | `cubic-bezier(0.22, 1, 0.36, 1)` | entering, opening, moving, resizing |
| `--ease-in-out` | `cubic-bezier(0.65, 0, 0.35, 1)` | an element moving on screen from A to B (board card, sidebar snap) |
| `--ease-bounce` | `cubic-bezier(0.34, 1.36, 0.64, 1)` | only the toggle thumb and the count badge pop |
| `--ease-drawer` | `cubic-bezier(0.32, 0.72, 0, 1)` | the phone drawer and bottom sheets |
| `--scale-dialog` | 0.96 | dialog open and close |
| `--scale-menu` | 0.97 | menu and popover open |
| `--scale-menu-close` | 0.99 | menu and popover close |
| `--scale-tooltip` | 0.98 | tooltip open |
| `--scale-press` | 0.97 | button and icon-button `:active` |
| `--distance-micro` | 4px | text swap, route content rise |
| `--distance-base` | 8px | page slide, toast rise (16px on phones) |
| `--blur-small` | 2px | text swap, icon swap, skeleton reveal |
| `--blur-pane` | 3px | the incoming pane of a list ↔ detail slide |

## The catalogue: every component and its transition

The recipe column names the transitions.dev recipe the implementation follows (from the `transitions-dev` skill), adapted to our tokens.

| Component / event | What moves | Duration and easing | Recipe |
|---|---|---|---|
| Button, icon button, segmented button, nav row: press | `scale(var(--scale-press))` on `:active` (the `press` utility, used by `Button` and `SegmentedControl`) | `--duration-micro` `--ease-out` | — |
| Hover on anything | background, border and text colour only | `--duration-quick` `ease` | — |
| Menu, select, popover, combobox (org menu, account menu, factory picker, Move menu, filters, `Select`) | open: opacity 0→1 and `scale(0.97)`→1 from the trigger; close: opacity and `scale(0.99)` | open `--duration-fast`, close `--duration-quick`, `--ease-out` | menu-dropdown (05) |
| Tooltip | delayed 80ms in with fade and `scale(0.98)`; instant out; once one is open, the next opens with no delay (`Tooltip`, `TooltipProvider`) | in `--duration-quick` `ease-out` | tooltip (17) |
| Dialog (New item, confirm, publish, create organization) | overlay fades; panel opacity and `scale(0.96)`→1 from centre; close is the reverse, faster | open `--duration-fast`, close `--duration-quick`, `--ease-out` | modal (06) |
| Sheet and phone drawer | slide from its edge with fade | open `--duration-slow`, close `--duration-medium`, `--ease-drawer` | panel-reveal (07) |
| Sidebar collapse / expand and the snap to the rail | width via a transform on an inner wrapper, labels fade | `--duration-fast` `--ease-in-out`; none while dragging | card-resize (01) |
| ⌘K search | no open animation; results update in place | instant | — |
| Keyboard shortcuts list, opened by ? (`ShortcutsDialog`, `instant`) | no transition, in or out: a key opens it, and rule 1 says keyboard actions never animate | instant | — |
| Keyboard shortcuts list, opened from the account menu | as a dialog: overlay fades, panel scales from the centre | dialog row | modal (06) |
| Route change (any page) | the old content leaves at once (its snapshot is hidden, never cross-faded); the new content area fades in and rises 4px; the shell never moves | `--duration-quick` `--ease-out`; first five blocks staggered by `--duration-stagger` | texts-reveal (18), shortened |
| List ↔ detail inside a page (Needs you on a phone, a two-pane picker on a phone, onboarding steps) | the incoming pane slides 8px with a 3px blur cross-fade in the direction of travel | `--duration-fast` `--ease-out` | page-side-by-side (08) |
| Tabs (Item tabs, Jonah tabs, Account side nav) and segmented controls (time window, Board/List, Form/YAML) | the selected pill or underline slides to the new tab; the content swaps with a quick fade | pill `--duration-fast` `--ease-out`; content `--duration-quick` | tabs-sliding (16) |
| Phone tab bar indicator (the 2px ink line on the current tab of the bottom tab bar) | the indicator fades in on the new current tab and out on the old one; the label and icon colour change with it; nothing moves, so reduced motion keeps the same fade | indicator `--duration-fast` `--ease-out`; colour `--duration-quick`; the fade is the only change | tabs-sliding (16), fade only |
| Accordion and disclosure (tool-call details, "Show transcript", settings groups, sidebar factory groups), built as `ui/disclosure.tsx` | height via `grid-template-rows` 0fr→1fr; chevron rotates | `--duration-fast` `--ease-out` | accordion (21) |
| Switch | thumb travels with a slight overshoot; track colour fades (`Switch`) | thumb `--duration-medium` `--ease-bounce`; track `--duration-quick` | toggle (27) |
| Checkbox (bulk select, notification matrix) | box fills, then the check draws (`Checkbox`) | box `--duration-quick`, check `--duration-medium` after the box | checkbox-check (25) |
| Toast (sonner) | rises 8px with fade and `scale(0.97)`; stacked toasts push back | open `--duration-medium`, close `--duration-fast`, `--ease-out` | toast (22), banner-stacking (32) |
| Board card moves to another column or lane (live update or a person's drag) | the card travels from its old box to its new one (FLIP / View Transitions); other cards in both columns reflow | `--duration-fast` `--ease-in-out` | card-resize (01) |
| A new item appears in a list or column | fades in and rises 4px; never shifts the list it lands in without a reflow transition | `--duration-fast` `--ease-out` | texts-reveal (18) |
| The Board's Expedite row (drawn in every column while the line has an expedite lane, empty or not) | no entrance: it is there with the board; its tint and cards change with the lane's use, so only its colour fades; cards that join it use the row above | `--duration-quick` colour | — |
| A status changes in place ("Running" → "Sent back", a chip) | new text in with a 4px move and 2px blur (`Chip`) | `--duration-quick` `--ease-in-out` | text-states-swap (04) or text-morph (33) |
| A number changes (KPIs, counts, cost, the needs-you count) | changed digits pop in from below with blur (`NumberRoll`) | `--duration-emphasis` `--ease-bounce`, 40ms per digit; frequency decides: a value that changes again within 8 s of its last change (`QUIET_MS`) swaps in place with no animation, so a figure that updates every few seconds never shows two digits | number-pop-in (02) |
| A chart bar or meter fill changes value (`Bars`, `TargetBar`, `Meter`, the KPI meter, the Spend bars) | the fill scales from its baseline (scaleY for columns, scaleX for rows) inside a clipping track; never `height` or `width`; none under reduced motion | `--duration-fast` `--ease-out` | card-resize (01) |
| Segmented progress meter (Items assembly row) | a segment fades its background between `rule` and `ink` when a part merges; no movement | `--duration-quick` `--ease-out`; none under `prefers-reduced-motion` | — |
| The needs-you count badge appears in the sidebar | slides in and pops | `--duration-emphasis` `--ease-bounce` | notification-badge (03) |
| Loading: any route or panel waiting on data more than 100ms | a skeleton of the real layout pulses once, then cross-fades with a 2px blur to the content (`Skeleton`) | pulse `--duration-pulse` linear; reveal `--duration-slow` | skeleton-reveal (14) |
| An agent working (item header, run row, Floor activity) | the status line shimmers ("Writing the spec…") and swaps to the next state | shimmer 2s linear; swap `--duration-quick` | thinking-states (28) |
| Streaming agent output in Watch live | words resolve through a 1px blur | 60ms per word, `--duration-medium` fade | streaming-text (30) |
| Invalid form field | a short horizontal shake; the message fades in | segments `--duration-micro` | error-state-shake (12) |
| Approve, publish, merge succeed | the button's label swaps to a check | `--duration-emphasis` `--ease-out` | success-check (10) + icon-swap (09) |
| Andon pulled | the red strip slides down from the top bar; the line's stations dim | `--duration-fast` `--ease-out` | panel-reveal (07), shortened |
| Copy to clipboard | the icon swaps to a check and back after 1.5s | `--duration-fast` | icon-swap (09) |
| Problems panel rows and count while typing (YAML view) | instant, no transition, because they follow typing (frequency decides) | — | — |
| Drag (board, definition stations, sidebar width) | the dragged element follows the pointer 1:1 with no easing; on drop it settles | settle `--duration-fast` `--ease-out` | — |

Anything not in this table: pick the closest row, add it to the table in the same pull request, and say why in the PR.

## How to build it

- Put the tokens in `styles.css` `@theme`, and expose them to Tailwind as `duration-*` and `ease-*` utilities. Components use the utilities or `var(--…)`.
- Radix primitives: animate on `[data-state=open]` and `[data-state=closed]`, using Radix's `--radix-popover-content-transform-origin` (and the equivalents for dropdown, select and tooltip) as the `transform-origin`.
- Route changes: TanStack Router's `defaultViewTransition` with a `::view-transition-new(main)` fade and rise on the content region only (`view-transition-name: main`). Every old snapshot (`root`, `sidebar`, `topbar`, `main`) is `display: none`, the groups do not animate and the new pictures blend as `normal`, so two pages are never legible together (the browser default would add them with `plus-lighter`). The sidebar and top bar keep their own names so they never cross-fade. The rules apply only to a router transition (view transition type `route`, `defaultViewTransition: { types: ['route'] }`), so a board card move leaves the page around its cards alone. Under reduced motion the new content fades by opacity only.
- Layout moves (board cards): `document.startViewTransition` with a `view-transition-name` per card (`card-<ref>`), or FLIP where View Transitions are unavailable.
- Numbers: `NumberRoll` (`apps/web/src/ui/number-roll.tsx`), our own component on the number-pop-in recipe, used by the stat strip, counts and costs.
- Skeletons: one `Skeleton` component (`apps/web/src/ui/skeleton.tsx`), and every route's `pendingComponent` renders the page's skeleton (`pendingMs: 100`, `pendingMinMs: 300`) instead of a blank page.
- No new animation library unless a recipe truly needs springs. CSS and the View Transitions API cover this catalogue.

## Checklist (reviewer and author)

- [ ] Every element that appears, disappears, changes size or changes state has its catalogue transition.
- [ ] Nothing animates on keyboard-driven, high-frequency actions.
- [ ] Durations and easings are tokens; no `transition: all`.
- [ ] Only transform, opacity and small blur animate.
- [ ] Menus and popovers grow from their trigger; dialogs from the centre.
- [ ] Exits are faster than entrances.
- [ ] Reduced motion keeps the state change, drops the movement.
- [ ] A slow route shows its skeleton, never a blank page.
- [ ] Run the `review-animations` skill on the diff.
