# Goal design system

The one source for how Goal looks, reads and moves. Version 3, "Plain", replaces the 2026-10-26 revamp (cobalt, violet, Archivo and the navy andon panel) because it read as generic AI product UI. The owner approved it on 2026-11-01. The screens are on the owner's Goal UI canvas and exported to [screens/](screens/README.md). Motion has its own file: [MOTION.md](MOTION.md), and it is as binding as this one.

> **LOGIC:** when building a page, check `docs/design/pages/<page>.md` first. A rule there overrides this file. Otherwise, follow this file and MOTION.md exactly.

## Principles

- **Quiet by default, loud only for meaning.** The interface is neutral: white, near-black text, grey rules. Colour appears only where it says something (running, needs you, sent back, constraint, done), and mostly as a small dot, a bar or a number, not a filled block.
- **Contrast before colour.** The primary action is a near-black button. Selection is a grey fill. Emphasis comes from weight, size and position before hue.
- **Density is a feature.** People scan lines, boards and lists all day. Use 13–14px text, 32px controls, tight rows and hairline rules, not big padded cards.
- **Show it, don't write it.** A number with its target, a bar, a list or a timeline before a sentence. No insight paragraphs, no eyebrow labels, no decorative icon tiles.
- **People act where they read.** Approve, Deny, Reply sit on the row or card that asks. One primary action per screen.
- **Things move because someone acted** (MOTION.md). Nothing appears, disappears or changes size without a short transition, and nothing moves on its own except a running dot.

## What we never do (the slop list)

The review rejects a screen that has any of these:
- gradients, glows, glassmorphism, coloured or "tinted brand" shadows, noise or grain;
- purple or indigo as a brand colour; saturated colour fills behind large areas;
- a coloured left-border accent on cards or callouts;
- emoji, sparkle icons, or a "magic"/AI glyph for agent features;
- icon tiles (an icon inside a tinted rounded square) as decoration in lists, timelines or empty states;
- rows of more than four KPI tiles, or a KPI without a target or a comparison;
- ALL-CAPS labels, eyebrow text above headings, "A · B · C" strings;
- pill-shaped everything: radius above 10px anywhere except avatars, dots and dialogs (12px);
- a dark hero panel inside a light page (the old navy andon panel);
- copy that explains the UI ("Here you can…", "Easily manage…"); filler empty states;
- stretched or display typefaces in the app UI.

## Colour

Tokens keep their names across themes, and components use token names only (`apps/web/src/ui/raw-colors.test.ts` fails on a raw hex, `rgb()` or `hsl()`). Inverted fills (the primary button, org mark, skip link, toast) use `bg-action text-action-ink`, never `bg-ink text-white`: `ink` is near-white in dark. `text-white` is allowed only on a fill that is dark in both themes (`run`, `you`, `rework`, `mark`). The same test fails on `bg-ink`/`bg-action` with `text-white`, `hover:bg-ink/`, `bg-white`, `bg-black` and `text-black`; `theme-contrast.test.ts` checks the pairs at 4.5:1 in both themes.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--color-shell` | `#FFFFFF` | `#0F0F0F` | Page and content background, cards |
| `--color-sidebar` | `#F8F8F7` | `#141414` | Sidebar |
| `--color-panel` | `#FAFAF9` | `#171717` | Board cells, table header, quiet wells |
| `--color-hover` | `#F0F0EE` | `#1F1F1F` | Hover, selected nav, segmented track |
| `--color-line` | `#EAEAE7` | `#262626` | Card borders, dividers, table rules |
| `--color-rule` | `#D6D6D2` | `#363636` | Input and secondary button borders |
| `--color-ink` | `#171717` | `#EDEDEC` | Text, headings, the primary button fill |
| `--color-ink-soft` | `#4A4A47` | `#B4B4B0` | Secondary text |
| `--color-muted` | `#6B6B67` | `#8F8F8A` | Meta text, icons (5.3:1 on white, 4.9:1 on the sidebar) |
| `--color-mark` | `#171717` | `#171717` | Source tiles (Knowledge, triggers): the near-black square under a white icon, the same in both themes, with a 1px inset white rule at 12% |
| `--color-preview-light`, `--color-preview-light-side` | `#FFFFFF`, `#F8F8F7` | same | The Light half of an Appearance swatch (content, sidebar); never theme-following |
| `--color-preview-dark`, `--color-preview-dark-side` | `#0F0F0F`, `#141414` | same | The Dark half of an Appearance swatch (content, sidebar); never theme-following |
| `--color-action` | `#171717` | `#EDEDEC` | Primary buttons (text `--color-action-ink`: white / `#111111`) |
| `--color-run` | `#2B5BD7` | `#3D6BE0` | Running fills and marks, focus ring, the selection |
| `--color-link` | `#2B5BD7` | `#7396F2` | Link and action text (`text-link`) |
| `--color-run-ink` | `#1E46B0` | `#A9C1FA` | Link hover, text on the run tint |
| `--color-run-tint` | `#EEF2FC` | `#17213A` | Text selection, a running item's highlight |
| `--color-you` | `#C2410C` | `#B84D1C` | Waiting on a person: counts, the Approve button (white text 5.2:1) |
| `--color-you-tint` | `#FDF2EC` | `#341C10` | A question or approval waiting; selected Needs you row |
| `--color-you-ink` | `#9A3412` | `#FDBA94` | Text on the orange tint |
| `--color-rework` | `#C93030` | `#C73A3A` | Sent back, andon, destructive (white text 4.9:1) |
| `--color-rework-tint` | `#FDF0F0` | `#361616` | Sent-back and andon chips, the andon strip |
| `--color-rework-ink` | `#A42525` | `#FCA5A5` | Red text |
| `--color-amber` | `#EAB308` | `#EAB308` | The constraint only: its marker and busy bar |
| `--color-amber-tint` | `#FEF8E1` | `#30270C` | The expedite lane |
| `--color-amber-ink` | `#6F5000` | `#F5D46B` | Text on the amber tint |
| `--color-ok` | `#1F8A4C` | `#2E9E5E` | Done and healthy marks (dots, bars) |
| `--color-ok-tint` | `#EEF7F1` | `#11291B` | Rarely: a merged confirmation |
| `--color-ok-ink` | `#17663A` | `#7EDBA4` | Healthy text, good deltas |

Text uses the ink and link tokens; fills and marks (dots, bars, borders, rings) use the base tokens. A base token as a text colour fails AA in one theme, and `src/ui/text-tokens.test.ts` rejects it. Measured contrast of each text token, on card then panel (`src/ui/theme-contrast.test.ts` holds each at 4.5:1 or more, also on shell):

| Token | Light (card / panel) | Dark (card / panel) |
|---|---|---|
| `--color-link` | 5.87 / 5.62 | 6.73 / 6.29 |
| `--color-run-ink` | 8.21 / 7.86 | 10.68 / 9.98 |
| `--color-rework-ink` | 7.32 / 7.01 | 10.10 / 9.45 |
| `--color-ok-ink` | 7.00 / 6.71 | 11.46 / 10.72 |
| `--color-you-ink` | 7.31 / 7.00 | 11.53 / 10.78 |

The `--color-andon*` tokens are removed.

Rules:
- Running is blue. Waiting on you is orange. Sent back, andon and destructive are red. The constraint is yellow, and nothing else is. Done is green. Blue and orange are the pair that must always be told apart, and they also differ in lightness.
- Status chips are neutral (white with a line border) carrying a 6px coloured dot. Only "needs you" and "sent back" chips get a tint fill, because they ask for action.
- Charts: the current period in `ink`, past periods in `rule`, the target as a dashed `muted` line with its label, the constraint in `amber`. Categorical series use the factory colours.
- **Factory colours** are a factory's identity on its 18px letter badge only: cobalt `#2B5BD7`, teal `#0E7C6E`, rust `#B4461A`, green `#1F7A3A`, ocean `#0B6493`, ochre `#7E5A00`, slate `#55595F`, plum `#6B3A5E`.
- **The Goal mark** is a near-black rounded square (`#171717`, 6px radius, a 1px inner white rule at 12% so it holds on dark) with a white G ending in a yellow node. It is the only place the yellow node appears outside the constraint.

## Typography

- **IBM Plex Sans** (`@fontsource/ibm-plex-sans`, weights 400, 500, 600 and italic 400) for all text. Its engineered, slightly industrial shapes suit a factory; it is not the default every AI tool picks.
- **IBM Plex Mono** (`@fontsource/ibm-plex-mono`, 400 and 500) for code, paths, commands, YAML, refs (#147), keyboard shortcuts and tool names.
- Tabular numerals everywhere.
- Scale:
  - 12px: meta, captions, chips.
  - 13px: secondary text, tables, sidebar sub-items, buttons.
  - 14px: body and controls.
  - 14px at 600: section headings.
  - 18px at 600: detail titles.
  - 20px at 600, letter-spacing −0.01em: page titles.
  - 24px at 500: KPI numbers, with the unit at 13px in `muted`.
- Weights: 400 for text, 500 for controls, nav and numbers, 600 for headings. Nothing heavier.
- Sentence case. Never ALL CAPS, never eyebrow labels, never "A · B" strings.

## Shape, space and elevation

- Radii: 4px chips and badges; 6px buttons, inputs, nav items and menus' items; 8px cards, board cells, popovers and menus; 12px dialogs and sheets; fully rounded for dots, avatars and count pills (the orange 18px count); one 18px letter badge for factories.
- Spacing: a 4px base, with 8, 12, 16, 20, 24 and 32px between blocks. Content padding is 24px 28px on desktop and 16px on phones.
- Elevation: borders, not shadows. Popovers, menus, dialogs and toasts share one shadow, `--shadow-pop` (`0 0 0 1px` ink at 6% plus `0 10px 28px -10px` ink at 18%; on dark the ring is white at 8%). The selected segment gets a 1px hairline shadow.
- Icons: lucide at 16px, stroke 1.75, in `muted`; `ink` when active. Never in a tinted tile.

## The shell

- The app fills the viewport. The sidebar is pinned left, full height, on `--color-sidebar` with a 1px right rule. There is no framed shell.
- **Sidebar** (248px by default; resizable 216–400px; collapses to a 56px rail; the existing resize behaviour is unchanged, with the snap animated per MOTION.md):
  - **Top row:** the Goal mark and the organization's name open the organization menu; Search (⌘K) and Collapse on the right.
  - **New item:** a secondary button (white, rule border, ink text) with a `C` key hint in a mono keycap. Not a filled brand block.
  - **Organization-wide:** Home, Needs you (orange count), Runs, Access, Secrets, Webhooks, Knowledge. 32px rows, 16px icons. The current page is a grey `--color-hover` fill with ink text and an ink icon.
  - **Factories:** each factory is a collapsible group: its 18px letter badge, its name, and an orange count (needs you) or a red dot (andon). The open factory's pages (Floor, Board, Items, Agents, Intake, Jonah, Definition, Settings) sit under a 1px guide line; the current one is a grey fill.
  - **Bottom:** spend this week as a plain row with a 4px bar (no card), then the person.
- **Top bar** (52px): the breadcrumb (muted ancestors, ink current) on the left; the page's actions on the right, the primary one last. At tablet widths, when the actions do not fit beside the crumbs, they wrap to a right-aligned row under the crumbs (no animation); the crumbs keep at least their last item, ellipsized.
- **Phones** (below 768px): a top bar with the mark (opens the drawer), the title and actions; a bottom tab bar (Needs you, Floor, Board, Items, More) with a 2px ink indicator on the current tab. Targets are at least 44px.

## Vocabulary

Unchanged from version 2: New item, Needs you (Approval, Decision, Question, Alert), Intake, Trigger, Agents, Jonah (Overview, Proposals, Tests), Credentials, Connections, Hands-off, Sent back; agents named by role. Keep Floor, Board, Items, line, station, lane, WIP, buffer, expedite lane, constraint, andon, assembly, part, touch and definition as the [glossary](../reference/glossary.md) defines them.

## Components

- **Buttons:** 32px tall (28px small), 6px radius, 13px at 500. Below 768px, small and default buttons are at least 44px tall (`max-md:min-h-11`); the desktop sizes do not change.
  - Primary: `--color-action` fill, `--color-action-ink` text.
  - Secondary: shell fill, `rule` border; hover darkens the border to `muted`.
  - Danger: red text; a red fill only for the final confirmation in a dialog.
  - Approve inside Needs you: orange fill, white text.
  - Icon buttons: 32px, `aria-label`, a 44px hit area on a coarse pointer, muted icon that turns ink on hover.
  - Every button scales to 0.97 while pressed (MOTION.md).
- **Chips:** 22px, 4px radius, 12px at 500, white with a line border and a 6px dot for status. Tinted only for needs you (orange) and sent back (red).
- **Segmented control:** a `hover`-grey track with no border, 2px inset; the selected segment is white with a hairline shadow, and the highlight slides between segments.
- **Truncated labels:** a label that truncates carries a `title` with its full text (or wraps); `truncate-title.test.ts` guards it.
- **Inputs:** 32px from 768px, 44px below it, selects included (40px on onboarding), 6px radius, `rule` border, focus: `run` border plus a 3px `run-tint` ring.
- **Switch:** 32×18px track; off `rule`, on `ink`; the thumb travels (MOTION.md, Toggle).
- **Stat strip** (replaces the KPI tiles): up to four numbers in one row separated by 1px vertical rules, no boxes. Each has a 12px muted label, the 24px number and unit, and one meta line: the target, and the signed delta in `ok-ink` or `rework-ink`. A 2px bar under the number shows progress to target only when there is one.
- **The line** (replaces the andon panel): a plain section on the page, no dark panel. Each station is a column: its name at 13px/500, the item count at 24px/500, a 4px load bar (ink for busy, `rule` for free capacity, amber for the constraint), the agent slots as 8px squares (ink working, orange waiting on you, outlined free) and what waits as muted text. Stations are joined by a 1px rule with a small chevron. The constraint gets a yellow 6px marker and the word "Constraint". When the andon is pulled, a red-tint strip spans the top of the Floor with the reason, who pulled it and Clear.
- **Board column:** a header row (name, "n of WIP" with a 4px segmented meter, agents and busy share) over a `panel` cell; no header card. The constraint's header has a yellow marker. The expedite lane is a row with the amber tint above the standard lane. A station with a buffer (Build) shows its **In progress** cards first, at the top of the column, then the **Ready (buffer)** cards below them, dimmed with the `panel` surface instead of `card` (never opacity, which fails text contrast), under their own "Ready, buffer n of m" label; "Show N more" belongs to the In progress group. The same order holds on phones.
- **Item card:** white, 8px radius, line border, 12px padding: the ref in mono and the type, the title (two lines), the assembly line, the agent and cost, and one status chip. A card waiting on you has a 1px orange border and an orange dot, not a glow. Cards lift nothing on hover; the border darkens.
- **Run timeline:** runs are plain sections with a header (station, agent, outcome chip, duration, turns, cost). Events are rows with the time in mono, a 6px dot on a 1px vertical rule (red for a failed check, green for passed, orange for a person), the text and a meta column.
- **Needs you list:** rows with the factory badge, the type as a word in its colour, the title, item and station, the age. The selected row has the orange tint and no edge bar.
- **Sentence builder, two-pane picker, definition editor, stepper:** as in version 2, restyled with these tokens. The stepper uses 8px dots on a 1px track: ink for done, an ink ring for current (orange ring when it waits on you).
- **Empty states:** one sentence that says what to do and one action. No illustration, no icon tile.
- **Menus and popovers:** 8px radius, `--shadow-pop`, 4px padding, 30px items with 6px radius and a grey hover.
- **Toasts:** bottom-right, ink fill with white text in light mode (inverted in dark), 8px radius, one line plus an optional action.

## Screens

The screens on the canvas, one page per flow (unchanged list from version 2): Get started (sign up, the code, eight onboarding steps, ready), Daily work (Home, Floor, Board, Work item, Needs you, Items, Runs), Intake (Intake, New trigger, Trigger, Webhooks), Build a factory (Agents, Agent, Definition, Review and publish, Factory settings), Jonah (Overview, Proposals, Proposal, Tests), Connections and knowledge, Organization, Sidebar and account, Mobile. Controls for features not built yet render disabled with "Not available yet" (ADR 0016, decision 4).

## Accessibility and checks

- WCAG 2.2 AA in both themes: text 4.5:1 (3:1 at 24px+), controls 3:1. Colours that must be told apart also differ in lightness.
- Real elements: `<button>`, `<a href>`, `<input>` with `<label>`, `role="switch"`. Focus rings are 2px `run` with a 2px offset. Every drag has a Move menu, and every shortcut is shown where its action is.
- Phone-first: check 375, 768 and 1440px. Below 1024px wide tables scroll in their own box; from 1024px every column shows and long titles end in an ellipsis.
- Every visual change runs the ui-ux-pro-max pre-delivery checklist and the MOTION.md checklist.
