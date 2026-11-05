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

| Component / event | What moves | Duration and easing | Recipe | Where it lives |
|---|---|---|---|---|
| Button, icon button, segmented button, nav row: press | `scale(var(--scale-press))` on `:active` (the `press` utility, used by `Button` and `SegmentedControl`) | `--duration-micro` `--ease-out` | — | `ui/button.tsx`, `ui/segmented-control.tsx`; the `press` utility in `styles.css` |
| Hover on anything | background, border and text colour only | `--duration-quick` `ease` | — | `styles.css` and the `transition-colors` classes of each component |
| Menu, select, popover, combobox (org menu, account menu, factory picker, Move menu, filters, `Select`) | open: opacity 0→1 and `scale(0.97)`→1 from the trigger; close: opacity and `scale(0.99)` | open `--duration-fast`, close `--duration-quick`, `--ease-out` | menu-dropdown (05) | `ui/menu.tsx`, `ui/select.tsx`, `ui/popover.tsx`, `ui/date-range.tsx`; `.motion-panel` and `.motion-menu` in `styles.css` (`features/secrets`: the factory scope and Reminder in `parts.tsx`); the Slack secret select of `features/org-settings/notifications-section.tsx`; the ⋯ of a Coming up row on phones (`features/intake/coming-up.tsx`, Start now); the secret pickers of `features/factory-editor/environments/parts.tsx` and `features/factory-editor/add-tool/tool-parts.tsx` |
| Tooltip | delayed 80ms in with fade and `scale(0.98)`; instant out; once one is open, the next opens with no delay (`Tooltip`, `TooltipProvider`) | in `--duration-quick` `ease-out` | tooltip (17) | `ui/tooltip.tsx`; `ui/info-tip.tsx` (the ⓘ beside a heading, with a wrapping tip; a tap opens it on phones, on Intake, the item's Follow-ups, New item's Start, a check and Webhooks); `--animate-tooltip-in`; the ⓘ beside each heading of the trigger pages (`features/jonah/info-tip.tsx` in `features/intake/form-parts.tsx`, `trigger-check-section.tsx` and `trigger-check-log.tsx`) |
| Dialog (New item, confirm, publish, create organization) | overlay fades; panel opacity and `scale(0.96)`→1 from centre; close is the reverse, faster | open `--duration-fast`, close `--duration-quick`, `--ease-out` | modal (06) | `ui/dialog.tsx`, `ui/confirm-dialog.tsx`; `.motion-panel` and `.motion-dialog` in `styles.css` (Replace value and Delete in `features/secrets/secret-dialogs.tsx`) |
| Sheet and phone drawer | slide from its edge with fade | open `--duration-slow`, close `--duration-medium`, `--ease-drawer` | panel-reveal (07) | `ui/sheet.tsx`, `ui/drawer.tsx`, `app/phone-chrome.tsx`; `.motion-sheet` and `.motion-drawer` in `styles.css` (the New secret sheet is `features/secrets/new-secret-sheet.tsx`, on `ui/drawer.tsx`); the New secret sheet also opens from `features/factory-editor/environments/` (a variable, a file, Move to Secrets) and from Add a tool's Custom |
| Sidebar collapse / expand and the snap to the rail | width via a transform on an inner wrapper, labels fade | `--duration-fast` `--ease-in-out`; none while dragging | card-resize (01) | `app/sidebar.tsx`, `app/sidebar-rail.tsx`, `app/sidebar-resize-handle.tsx`, `shared/sidebar-motion.ts`; `.motion-sidebar-resize` and `.motion-sidebar` in `styles.css` |
| ⌘K search | no open animation; results update in place | instant | — | `app/sidebar-search.tsx` |
| Keyboard shortcuts list, opened by ? (`ShortcutsDialog`, `instant`) | no transition, in or out: a key opens it, and rule 1 says keyboard actions never animate | instant | — | `app/shortcuts-dialog.tsx`, `ui/dialog.tsx` |
| Keyboard shortcuts list, opened from the account menu | as a dialog: overlay fades, panel scales from the centre | dialog row | modal (06) | `app/account-menu.tsx`, `app/shortcuts-dialog.tsx`, `ui/dialog.tsx` |
| Route change (any page) | the old content leaves at once (its snapshot is hidden, never cross-faded); the new content area fades in and rises 4px; the shell never moves | `--duration-quick` `--ease-out`; first five blocks staggered by `--duration-stagger` | texts-reveal (18), shortened | `app/router-options.ts`, `app/view-transition.ts`, `app/app-shell.tsx`; `.shell-main` and `view-route-in` in `styles.css` |
| List ↔ detail inside a page (Needs you on a phone, a two-pane picker on a phone, onboarding steps) | the incoming pane slides 8px with a 3px blur cross-fade in the direction of travel | `--duration-fast` `--ease-out` | page-side-by-side (08) | `features/needs-you/needs-you-page.tsx`, `features/connections/new-connection-page.tsx`; `--animate-pane-forward` and `--animate-pane-back` |
| Tabs (Item tabs, Jonah tabs, Agent page tabs, Account side nav) and segmented controls (time window, Board/List, Form/YAML) | the selected pill or underline slides to the new tab; the content swaps with a quick fade; a tab changed by keyboard moves the underline at once and the content does not fade | pill `--duration-fast` `--ease-out`; content `--duration-quick` | tabs-sliding (16) | `ui/tab-nav.tsx`, `ui/segmented-control.tsx`, `features/items/saved-views.tsx` (`features/secrets`: the filters and Replace or Skip; `features/access`: the view switch); `features/factory-editor/environments/` (the environment tabs, Allow, Ask first and Off) and `features/factory-editor/add-tool/` (Custom's type) |
| Phone tab bar indicator (the 2px ink line on the current tab of the bottom tab bar) | the indicator fades in on the new current tab and out on the old one; the label and icon colour change with it; nothing moves, so reduced motion keeps the same fade | indicator `--duration-fast` `--ease-out`; colour `--duration-quick`; the fade is the only change | tabs-sliding (16), fade only | `app/phone-chrome.tsx` |
| Accordion and disclosure (tool-call details, "Show transcript", sidebar factory groups), built as `ui/disclosure.tsx` | height via `grid-template-rows` 0fr→1fr; chevron rotates | `--duration-fast` `--ease-out` | accordion (21) | `ui/disclosure.tsx`; `.motion-disclosure` and `.motion-chevron` in `styles.css`; a server's tools in `features/factory-editor/environments/mcp-card.tsx` |
| A rule leaves a list after Revoke (a standing rule under its tool in the Environments MCP card) | the row's opacity goes to 0 and its height collapses (`grid-template-rows` 1fr→0fr) once the revoke is answered, then the list refreshes; none under `prefers-reduced-motion` (the row is gone at once). New row: no other row covers a list row leaving after a destructive action, and a leaving row is faster than an entering one | `--duration-quick` `--ease-out` (exit, faster than the `--duration-fast` entrance of a new item) | accordion (21), reversed | `features/factory-editor/environments/standing-rules.tsx`; `ui/leaving.tsx` (the shared row and its timing), used by `features/intake/coming-up.tsx` when Start now is answered (the hold is the success check's, `shared/start-now.tsx`) |
| A suggestion leaves the New trigger dialog's list after Turn on | the row's opacity goes to 0 and its height collapses (`grid-template-rows` 1fr→0fr) once the publish is answered, then intake is read again; none under `prefers-reduced-motion`. New row: the same recipe as a rule leaving after Revoke, listed on its own because the trigger is turned on, not removed | `--duration-quick` `--ease-out` | accordion (21), reversed | `ui/leaving.tsx`, used by `features/intake/new-trigger-dialog.tsx` |
| Switch | thumb travels with a slight overshoot; track colour fades (`Switch`) | thumb `--duration-medium` `--ease-bounce`; track `--duration-quick` | toggle (27) | `ui/switch.tsx` |
| Checkbox (bulk select, notification matrix) | box fills, then the check draws (`Checkbox`) | box `--duration-quick`, check `--duration-medium` after the box | checkbox-check (25) | `ui/checkbox.tsx`; `.check-box` in `styles.css` |
| Toast (sonner) | rises 8px with fade and `scale(0.97)`; stacked toasts push back | open `--duration-medium`, close `--duration-fast`, `--ease-out` | toast (22), banner-stacking (32) | `ui/toaster.tsx`; the sonner overrides in `styles.css` |
| Board card moves to another column or lane (live update or a person's drag) | the card travels from its old box to its new one (FLIP / View Transitions); other cards in both columns reflow | `--duration-fast` `--ease-in-out` | card-resize (01) | `features/board/card-move.ts`, `features/board/board-dnd.tsx`; `::view-transition-group(.board-card)` in `styles.css` |
| A new item appears in a list or column | fades in and rises 4px; never shifts the list it lands in without a reflow transition | `--duration-fast` `--ease-out` | texts-reveal (18) | `features/factory-editor/knowledge/knowledge-cards.tsx` (a fact or a source added to the draft), `features/board/board-card.tsx`, `features/floor/activity-card.tsx`, `features/items/assembly-rows.tsx`, `features/items/trace-tab.tsx` (steps that arrive while Watch live follows), `features/runs/runs-page.tsx`, `features/intake/coming-up.tsx` (a row of Coming up), `features/intake/trigger-check-log.tsx` (a check that arrives in Recent checks); `--animate-item-in` and `--animate-row-in` |
| The constraint marker moves to another station (Agents) | the yellow dot and "Constraint" fade in at the new station by opacity only; the old one leaves at once; nothing travels, so reduced motion needs no change | in `--duration-fast` `--ease-out` | texts-reveal (18) | `features/agents/agents-page.tsx` |
| The Board's Expedite boxes (one yellow box per column while the line has an expedite lane, empty or not; the phone draws the same tint around its lane) | no entrance: they are there with the board; their tint and cards change with the lane's use, so only the colour fades; cards that join a box use the row above | `--duration-quick` colour | — | `features/board/board-column.tsx`, `features/board/board-lane.tsx` |
| A status changes in place ("Running" → "Sent back", a chip) | new text in with a 4px move and 2px blur (`Chip`) | `--duration-quick` `--ease-in-out` | text-states-swap (04) or text-morph (33) | `ui/chip.tsx`; a follow-up's state on the item page (`features/items/follow-ups-card.tsx`); `--animate-text-states-swap` (`features/secrets/import-secrets-page.tsx`: New or Already exists); a server's Tested, Not tested or Failed in `features/factory-editor/environments/mcp-card.tsx`; the Slack test result in `features/org-settings/notifications-section.tsx` |
| A number changes (KPIs, counts, cost, the needs-you count) | changed digits pop in from below with blur (`NumberRoll`) | `--duration-emphasis` `--ease-bounce`, 40ms per digit; frequency decides: a value that changes again within 8 s of its last change (`QUIET_MS`) swaps in place with no animation, so a figure that updates every few seconds never shows two digits | number-pop-in (02) | `ui/number-roll.tsx`, used by `features/intake/trigger-check-log.tsx` (a check trigger's Today line); `--animate-number-pop-in` |
| A chart bar or meter fill changes value (`Bars`, `TargetBar`, `Meter`, the KPI meter, the Spend bars) | the fill scales from its baseline (scaleY for columns, scaleX for rows) inside a clipping track; never `height` or `width`; none under reduced motion | `--duration-fast` `--ease-out` | card-resize (01) | `ui/bars.tsx`, `ui/target-bar.tsx`, `ui/meter.tsx`, `ui/kpi-tile.tsx` |
| Segmented progress meter (the board's part pips, WIP meter and lane slots, the Items assembly row) | a segment fades its background between its tones when a part merges or a slot fills; no movement. When a segment would be under 4px wide the meter draws one bar instead, whose fill uses the meter-fill rule above (scaleX inside a clipping track) | segments `--duration-quick` `--ease-out`; bar `--duration-fast` `--ease-out`; none under `prefers-reduced-motion` | — | `ui/segment-meter.tsx`, `features/board/board-card.tsx`, `features/board/board-column.tsx`, `features/board/board-lane.tsx`, `features/items/assembly-rows.tsx` |
| The needs-you count badge appears in the sidebar | slides in and pops | `--duration-emphasis` `--ease-bounce` | notification-badge (03) | `ui/count.tsx`, `app/sidebar-rail.tsx`, `app/phone-chrome.tsx`; `--animate-notification-badge` |
| Loading: any route or panel waiting on data more than 100ms | a skeleton of the real layout pulses once, then cross-fades with a 2px blur to the content, and once ready the content alone sets the slot's height (`Skeleton`) | pulse `--duration-pulse` linear; reveal `--duration-slow` | skeleton-reveal (14) | `ui/skeleton.tsx`, `app/states.tsx`; `TriggerCheckSkeleton` in `app/route-skeletons.tsx` (a check trigger's page, and its log while it loads); `--animate-skeleton-pulse` (the Secrets routes: `ListPageSkeleton`, `SecretSkeleton` and `ImportSkeleton` in `app/route-skeletons.tsx`); the Notifications row of Organization settings and the Slack row of Factory settings (`features/factory-settings/notifications-section.tsx`); the Environments tabs and Add a tool's catalog (`Skeleton` in `features/factory-editor/environments/environments-section.tsx` and `features/factory-editor/add-tool/add-tool-page.tsx`) |
| An agent working (item header, run row, Floor activity) | the status line shimmers ("Writing the spec…") and swaps to the next state | shimmer 2s linear; swap `--duration-quick` | thinking-states (28) | `ui/working-line.tsx` (`WorkingLine`), used in `features/items/item-page.tsx`, `features/items/trace-view.tsx`, `features/runs/runs-page.tsx` and `features/board/board-card.tsx` |
| Streaming agent output in Watch live | words resolve through a 1px blur | 60ms per word, `--duration-medium` fade | streaming-text (30) | not built (Watch live does not exist) |
| Invalid form field | a short horizontal shake; the message fades in | segments `--duration-micro` | error-state-shake (12) | `ui/field.tsx`, `features/intake/form-parts.tsx` (the problems under a trigger's blocks); `--animate-error-shake` (`features/secrets`: Name and Value on the New secret sheet); a variable's name and a new environment's name in `features/factory-editor/environments/` |
| Approve, publish, merge succeed | the button's label swaps to a check | `--duration-emphasis` `--ease-out` | success-check (10) + icon-swap (09) | `features/factory-editor/review/publish-card.tsx`, `features/jonah/proposal-page.tsx`, `features/intake/trigger-page.tsx`, `features/knowledge/parts.tsx`, `ui/success-label.tsx` (the shared label and check), `features/needs-you/detail.tsx`, `features/needs-you/needs-you-page.tsx` (the hold before the entry leaves), `shared/item-actions.tsx` (decisions Approve, MergeDialog), `features/floor/needs-you-card.tsx`; `--animate-success-check`; Add to draft in `features/factory-editor/add-tool/tool-parts.tsx` |
| Andon pulled | the red strip slides down from above its place; the line's stations turn their ink to the muted token (a colour change, never opacity: it fails text contrast); Clear plays the reverse, faster; a line that loads stopped shows the strip with no entrance | open `--duration-fast`, close `--duration-quick`, `--ease-out`; stations' ink changes over `--duration-fast` | panel-reveal (07), shortened | `ui/line.tsx`, `features/factories/mini-line.tsx`; `.motion-panel` and `.motion-andon` in `styles.css` |
| Copy to clipboard | the icon swaps to a check and back after 1.5s | `--duration-fast` | icon-swap (09) | `ui/copy-button.tsx` (`--animate-icon-swap`), used by `features/webhooks/endpoint-card.tsx`, `features/webhooks/test-panel.tsx`, `features/webhooks/secret-dialog.tsx`, `shared/tokens/created-dialog.tsx`, `features/members/members-page.tsx` (invite link), `features/api-keys/api-keys-page.tsx` (MCP address) `features/floor/ways-in-card.tsx`, `features/factory-editor/basics/basics-section.tsx` (factory id), `features/factory-editor/yaml/yaml-view.tsx` (Copy) and `features/models/egress-card.tsx` (Copy all). `features/factory-editor/frame/conflict-dialog.tsx`'s "Use theirs" copies as a side effect and keeps its toast |
| Problems panel rows and count while typing (YAML view) | instant, no transition, because they follow typing (frequency decides) | — | — | `features/factory-editor/yaml/yaml-view.tsx` |
| Drag (board, definition stations, sidebar width) | the dragged element follows the pointer 1:1 with no easing; on drop it settles | settle `--duration-fast` `--ease-out` | — | `features/board/board-dnd.tsx`, `features/factory-editor/line/station-dnd.tsx`, `app/sidebar-resize-handle.tsx` |

A pull request that implements or moves a row updates that row's "Where it lives" cell.

Anything not in this table: pick the closest row, add it to the table in the same pull request, and say why in the PR.

## How to build it

- Put the tokens in `styles.css` `@theme`, and expose them to Tailwind as `duration-*` and `ease-*` utilities. Components use the utilities or `var(--…)`.
- Radix primitives: animate on `[data-state=open]` and `[data-state=closed]`, using Radix's `--radix-popover-content-transform-origin` (and the equivalents for dropdown, select and tooltip) as the `transform-origin`.
- Route changes: TanStack Router's `defaultViewTransition` with a `::view-transition-new(main)` fade and rise on the content region only (`view-transition-name: main`). Every old snapshot (`root`, `sidebar`, `topbar`, `main`) is `display: none`, the groups do not animate and the new pictures blend as `normal`, so two pages are never legible together (the browser default would add them with `plus-lighter`). The sidebar and top bar keep their own names so they never cross-fade. The rules apply only to a router transition (view transition type `route`, `defaultViewTransition: { types: ['route'] }`), so a board card move leaves the page around its cards alone. Under reduced motion the new content fades by opacity only.
- Every View Transition's `ready`, `finished` and `updateCallbackDone` promises are handled (`app/view-transition.ts` wraps `document.startViewTransition` once at boot; `card-move.ts` handles its own), so a skipped transition (a quick second navigation, a hidden tab) logs nothing.
- Layout moves (board cards): `document.startViewTransition` with a `view-transition-name` per card (`card-<ref>`), or FLIP where View Transitions are unavailable.
- Numbers: `NumberRoll` (`apps/web/src/ui/number-roll.tsx`), our own component on the number-pop-in recipe, used by the stat strip, counts and costs.
- Skeletons: one `Skeleton` component (`apps/web/src/ui/skeleton.tsx`); once ready, the content alone sets the slot's height, so content shorter than its placeholder shrinks the slot. Every route's `pendingComponent` renders the page's skeleton (`pendingMs: 100`, `pendingMinMs: 300`) instead of a blank page.
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
- [ ] A row this change implements or moves has its "Where it lives" cell updated.
- [ ] Run the `review-animations` skill on the diff.
