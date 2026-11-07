// Why a queued or ready item waits on another item, in words. The board card gets one short line
// (waitingShort) and its tooltip the full list (waitingDetail); the item page lists each need itself.
import type { ItemWaiting } from '../api/types';

/** The card's one line: "Waits for sre#3", or "Waits for sre#3 +2" when three items hold it. */
export function waitingShort(waiting: Pick<ItemWaiting, 'items'> & { kind?: ItemWaiting['kind'] }): string {
  const [first, ...rest] = waiting.items;
  if (!first) return 'Waiting';
  const more = rest.length > 0 ? ` +${rest.length}` : '';
  return waiting.kind === 'turn' ? `Waits for ${first} to land${more}` : `Waits for ${first}${more}`;
}

/** Where a turn wait lands: its integration branch, or "the default branch" when it has none. */
export function turnTarget(waiting: Pick<ItemWaiting, 'target'>): string {
  return waiting.target || 'the default branch';
}

/**
 * The full list for a tooltip, one line per need: its ref, its title and the paths that hold the
 * wait (the shared files for a files wait). Nothing is cut short here; the tooltip wraps it.
 */
export function waitingDetail(waiting: ItemWaiting): string {
  if (waiting.kind === 'turn') {
    return `Waiting for ${waiting.items.join(', ')} to land on ${turnTarget(waiting)}`;
  }
  const lines = waiting.needs.map(({ ref, title, paths }) => {
    const because = waiting.kind === 'files' ? 'same files' : 'moves';
    const where = paths.length > 0 ? ` (${because} ${paths.join(', ')})` : '';
    return `${ref} ${title}${where}`;
  });
  if (waiting.kind === 'files' && waiting.files.length > 0) {
    lines.push(`Shared files: ${waiting.files.join(', ')}`);
  }
  return lines.join('\n');
}
