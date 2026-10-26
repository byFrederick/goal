// Why a queued or ready item waits on another item, in words. The board card gets one short line
// (waitingShort) and its tooltip the full list (waitingDetail); the item page lists each need itself.
import type { ItemWaiting } from '../api/types';

/** The card's one line: "Waits for sre#3", or "Waits for sre#3 +2" when three items hold it. */
export function waitingShort(waiting: Pick<ItemWaiting, 'items'>): string {
  const [first, ...rest] = waiting.items;
  if (!first) return 'Waiting';
  return rest.length > 0 ? `Waits for ${first} +${rest.length}` : `Waits for ${first}`;
}

/**
 * The full list for a tooltip, one line per need: its ref, its title and the paths that hold the
 * wait (the shared files for a files wait). Nothing is cut short here; the tooltip wraps it.
 */
export function waitingDetail(waiting: ItemWaiting): string {
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
