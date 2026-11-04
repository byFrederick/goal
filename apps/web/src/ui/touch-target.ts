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

/**
 * For a short text link that is narrow as well as low (an "Edit" beside a value): 12 px more on each
 * side and 16 px above and below, taken back by the same negative margins, so the drawn text stays
 * where it was and the link answers across at least 44×44 px below 768 px.
 */
export const touchLinkWide = 'max-md:-mx-3 max-md:-my-4 max-md:px-3 max-md:py-4';
