// MASTER.md, Phones: every target is at least 44 px. These classes grow a control's hit area below
// 768 px without changing its drawn size or adding motion; the control keeps its own `press`.

/**
 * For a control drawn 28 px tall or more (a segment, a filter chip): a pseudo-element 8 px taller on
 * each side takes the clicks, so a 28 px segment answers across 44 px. Needs a `relative` parent
 * chain only through this class, which sets `relative` itself.
 */
export const touchTarget =
  'relative max-md:before:absolute max-md:before:-inset-y-2 max-md:before:inset-x-0 max-md:before:content-[""]';

/**
 * For a bare text link (a row title): 16 px of padding above and below, taken back by the same
 * negative margin, so the line stays where it was and the link answers across at least 44 px
 * (an 18 px line plus 16 px each side).
 */
export const touchLink = 'max-md:-my-4 max-md:py-4';
