# Design System Master File

> **Goal overrides (user decisions — these take precedence over the generated values below)**
>
> - **Palette:** the user's original "control room" palette replaces the generated navy/green one. No pink anywhere.
>
> | Token | Hex | Use |
> |---|---|---|
> | `--ground` | `#E6E9E7` | Concrete; reserved (no longer the page background) |
> | `--shell` | `#EEF1EF` | Page background: the app fills the viewport |
> | `--card` | `#F6F7F5` | Cards |
> | `--surface` | `#FFFFFF` | Items inside cards, inputs |
> | `--ink` | `#1D2733` | Text, primary buttons, active nav |
> | `--muted` | `#556270` | Secondary text (5.6:1 on card) |
> | `--rule` | `#CBD2CE` | Borders, dividers |
> | `--run` | `#1F6F8B` | Running / actual / positive data (teal) |
> | `--amber` | `#F0A202` | The constraint (drum) only |
> | `--you` | `#5B4BB7` | Waiting on a human |
> | `--rework` | `#B4441C` | Rework, delays, losses, andon (replaces red/pink) |
> | `--ok` | `#2E6B4E` | Healthy, done |
> | `--ok-tint` | `#E3EFE8` | Background behind healthy states |
> | `--ok-ink` | `#24563E` | Healthy text ("Running", positive deltas) |
> | `--you-ink` | `#4A3C9E` | Text on the violet tint (waiting-on-you banners) |
> | `--ink-soft` | `#3A4654` | Rail icons; a count's background on the active nav item |
> | `--rule-strong` | `#6F7B77` | Dashed "new" squares, hover borders, switch borders |
>
> - **Navigation (user decision, 2026-10-23):** a 72px rail on the far left with the organization mark at the top (opens the organization menu: switch organization, members and roles, connector catalog, credentials, settings), then one square per factory of that organization only (current one ink-filled; violet count = waiting on you, rust dot = blocked), a dashed + for New factory, and at the bottom Connector catalog, Credentials and the user. Inside a factory a 232px sidebar groups Work (Floor, Board, Items), Build (Line, Agents, Connections, Definition) and Learn (Knowledge, Jonah); organization pages use an organization sidebar instead. On a phone: a top row with the organization mark and factory squares, a bottom tab bar, and the organization menu as a bottom sheet. The UI canvas is the reference.
> - **Layout:** the app takes the whole viewport like a normal web app: rail and sidebar pinned to the left edge (full height, right borders), content beside it. No framed, floating or rounded outer shell and no outer margin around the app; the framed box in the reference video was only its presentation. (User override, 2026-10-23.)
> - **Radii (softer, not pill-shaped):** cards 12px, inner rows 8px, buttons and inputs 8px, chips 6px. No 999px pills except status dots.
> - **Data marks:** hatched teal = actual, hatched rust = delay/loss, hatched gray = target; every value is signed and labelled, never color alone (charts.csv: waterfall).
> - **Drag and drop:** every card also has a Move menu (ux-guidelines: Dragging Movements, WCAG 2.2).
> - **Typography:** Inter with optical sizing (`opsz` 14..32, so large text renders as Inter Display), weights 400 for display and numerals, 500 for headings, 400 body; tracking −0.035em on display sizes; `font-feature-settings: "cv11","ss01"`; tabular numerals. Fira Code only for code, YAML and file paths. (User override: match the reference font.)
> - **No gradients** on any background or card. Solid fills only.
> - **Reference look:** the "Margin" dashboard by @nizamdesign: large numerals, insight card, "Needs you" list.


> **LOGIC:** When building a specific page, first check `design-system/pages/[page-name].md`.
> If that file exists, its rules **override** this Master file.
> If not, strictly follow the rules below.

---

**Project:** Goal
**Generated:** 2026-10-23 13:08:32
**Category:** E-signature / Document Workflow
**Design Dials:** Variance 4/10 (Balanced / Modern) | Motion 3/10 (Subtle) | Density 8/10 (Dense / Dashboard)

---

## Global Rules

### Color Palette

| Role | Hex | CSS Variable |
|------|-----|--------------|
| Primary | `#1E3A5F` | `--color-primary` |
| On Primary | `#FFFFFF` | `--color-on-primary` |
| Secondary | `#2563EB` | `--color-secondary` |
| On Secondary | `#FFFFFF` | `--color-on-secondary` |
| Accent/CTA | `#16A34A` | `--color-accent` |
| On Accent/CTA | `#000000` | `--color-on-accent` |
| Background | `#F8FAFC` | `--color-background` |
| Foreground | `#0F172A` | `--color-foreground` |
| Card | `#FFFFFF` | `--color-card` |
| Card Foreground | `#0F172A` | `--color-card-foreground` |
| Muted | `#E9EEF5` | `--color-muted` |
| Muted Foreground | `#475569` | `--color-muted-foreground` |
| Border | `#CBD5E1` | `--color-border` |
| Destructive | `#DC2626` | `--color-destructive` |
| On Destructive | `#FFFFFF` | `--color-on-destructive` |
| Ring | `#1E3A5F` | `--color-ring` |

**Color Notes:** Trust navy + signature green + audit trail

### Typography

- **Heading Font:** Fira Code
- **Body Font:** Fira Sans
- **Mood:** dashboard, data, analytics, code, technical, precise
- **Google Fonts:** [Fira Code + Fira Sans](https://fonts.googleapis.com/css2?family=Fira+Code:wght@400;500;600;700&family=Fira+Sans:wght@300;400;500;600;700&display=swap)

**CSS Import:**
```css
@import url('https://fonts.googleapis.com/css2?family=Fira+Code:wght@400;500;600;700&family=Fira+Sans:wght@300;400;500;600;700&display=swap');
```

### Spacing Variables

*Density: 8/10 — Dense / Dashboard*

| Token | Value | Usage |
|-------|-------|-------|
| `--space-xs` | `2px` / `0.125rem` | Tight gaps |
| `--space-sm` | `4px` / `0.25rem` | Icon gaps, inline spacing |
| `--space-md` | `8px` / `0.5rem` | Standard padding |
| `--space-lg` | `12px` / `0.75rem` | Section padding |
| `--space-xl` | `16px` / `1rem` | Large gaps |
| `--space-2xl` | `24px` / `1.5rem` | Section margins |
| `--space-3xl` | `32px` / `2rem` | Hero padding |

### Shadow Depths

| Level | Value | Usage |
|-------|-------|-------|
| `--shadow-sm` | `0 1px 2px rgba(0,0,0,0.05)` | Subtle lift |
| `--shadow-md` | `0 4px 6px rgba(0,0,0,0.1)` | Cards, buttons |
| `--shadow-lg` | `0 10px 15px rgba(0,0,0,0.1)` | Modals, dropdowns |
| `--shadow-xl` | `0 20px 25px rgba(0,0,0,0.15)` | Hero images, featured cards |

---

## Component Specs

### Buttons

```css
/* Primary Button */
.btn-primary {
  background: #16A34A;
  color: #000000;
  padding: 12px 24px;
  border-radius: 8px;
  font-weight: 600;
  transition: all 200ms ease;
  cursor: pointer;
}

.btn-primary:hover {
  opacity: 0.9;
  transform: translateY(-1px);
}

/* Secondary Button */
.btn-secondary {
  background: transparent;
  color: #1E3A5F;
  border: 2px solid #1E3A5F;
  padding: 12px 24px;
  border-radius: 8px;
  font-weight: 600;
  transition: all 200ms ease;
  cursor: pointer;
}
```

### Cards

```css
.card {
  background: #F8FAFC;
  border-radius: 12px;
  padding: 24px;
  box-shadow: var(--shadow-md);
  transition: all 200ms ease;
  cursor: pointer;
}

.card:hover {
  box-shadow: var(--shadow-lg);
  transform: translateY(-2px);
}
```

### Inputs

```css
.input {
  padding: 12px 16px;
  border: 1px solid #E2E8F0;
  border-radius: 8px;
  font-size: 16px;
  transition: border-color 200ms ease;
}

.input:focus {
  border-color: #1E3A5F;
  outline: none;
  box-shadow: 0 0 0 3px #1E3A5F20;
}
```

### Modals

```css
.modal-overlay {
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(4px);
}

.modal {
  background: white;
  border-radius: 16px;
  padding: 32px;
  box-shadow: var(--shadow-xl);
  max-width: 500px;
  width: 90%;
}
```

---

## Style Guidelines

**Style:** Minimalism & Swiss Style

**Keywords:** Clean, simple, spacious, functional, white space, high contrast, geometric, sans-serif, grid-based, essential

**Best For:** Enterprise apps, dashboards, documentation sites, SaaS platforms, professional tools

**Key Effects:** Subtle hover (200-250ms), smooth transitions, sharp shadows if any, clear type hierarchy, fast loading

### Page Pattern

**Pattern Name:** Enterprise Gateway

- **Conversion Strategy:** Path selection (I am a...). Mega menu navigation. Trust signals prominent. Provide pause/stop for video and rotating logos; stop on focus and reduced motion. Logo carousel controls must be keyboard operable; pause moving media offscreen/hidden and render a static final state under reduced motion.
- **CTA Placement:** Contact Sales (Primary) + Login (Secondary)
- **Section Order:** Hero (Video/Mission) > Solutions by Industry > Solutions by Role > Client Logos > Contact Sales

---

## Motion

**Scroll Reveal** (Subtle) — Trigger: scroll (viewport enter) | Duration: 300-400ms | Easing: `power1.out`

```js
gsap.from(el, { opacity: 0, y: 12, duration: 0.35, ease: 'power1.out', scrollTrigger: { trigger: el, start: 'top 90%', toggleActions: 'play none none reverse' } });
```

**Framework notes:** Requires the ScrollTrigger plugin registered once via gsap.registerPlugin(ScrollTrigger); Use matchMedia('(prefers-reduced-motion: reduce)') to skip non-essential motion and render the final state immediately

- ✅ Keep the y offset small (8-16px) so it reads as a fade, not a slide
- ❌ Don't reveal below-the-fold content needed for SEO/crawlers as invisible-by-default without a no-JS fallback
- ⚡ toggleActions 'play none none reverse' avoids re-triggering on every scroll direction change

---

## Anti-Patterns (Do NOT Use)

- ❌ Confusing signing flow
- ❌ weak audit trail
- ❌ hidden consent

### Additional Forbidden Patterns

- ❌ **Emojis as icons** — Use SVG icons (Heroicons, Lucide, Simple Icons)
- ❌ **Missing cursor:pointer** — All clickable elements must have cursor:pointer
- ❌ **Layout-shifting hovers** — Avoid scale transforms that shift layout
- ❌ **Low contrast text** — Maintain 4.5:1 minimum contrast ratio
- ❌ **Instant state changes** — Always use transitions (150-300ms)
- ❌ **Invisible focus states** — Focus states must be visible for a11y

---

## Pre-Delivery Checklist

Before delivering any UI code, verify:

- [ ] No emojis used as icons (use SVG instead)
- [ ] All icons from consistent icon set (Heroicons/Lucide)
- [ ] `cursor-pointer` on all clickable elements
- [ ] Hover states with smooth transitions (150-300ms)
- [ ] Light mode: text contrast 4.5:1 minimum
- [ ] Focus states visible for keyboard navigation
- [ ] `prefers-reduced-motion` respected
- [ ] Responsive: 375px, 768px, 1024px, 1440px
- [ ] No content hidden behind fixed navbars
- [ ] No horizontal scroll on mobile
