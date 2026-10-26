// Arrow-key movement between the rail's items (C §10.3). Every item stays in the tab order; the
// arrows, Home and End are a shortcut along the rail, so this is a plain reducer, not a roving
// tabindex.

export type RailAxis = 'vertical' | 'horizontal';

/** The index to focus after `key` from item `index` of `count`, or null to let the key through. */
export function railFocusTarget(key: string, index: number, count: number, axis: RailAxis): number | null {
  if (count === 0) return null;
  const last = count - 1;
  const next = axis === 'vertical' ? 'ArrowDown' : 'ArrowRight';
  const previous = axis === 'vertical' ? 'ArrowUp' : 'ArrowLeft';

  switch (key) {
    case next:
      return index < 0 ? 0 : Math.min(index + 1, last);
    case previous:
      return index < 0 ? 0 : Math.max(index - 1, 0);
    case 'Home':
      return 0;
    case 'End':
      return last;
    default:
      return null;
  }
}
