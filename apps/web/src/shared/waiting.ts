// Why a queued or ready item waits on another item, in words. The board card gets one short line
// (waitingShort); waitingDetail is the tooltip of an item, files or turn wait, and the item page lists each
// need itself. A CI wait shows its detail behind CIWait's ⓘ.
import type { ItemWaiting } from '../api/types';

/** A commit's sha as people read it: its first 7 characters. */
export const shortSha = (sha: string): string => sha.slice(0, 7);

/** The card's one line: "Waits for sre#3", or "Waits for sre#3 +2" when three items hold it. */
export function waitingShort(
  waiting: Pick<ItemWaiting, 'items'> & { kind?: ItemWaiting['kind']; ci?: ItemWaiting['ci'] },
): string {
  if (waiting.kind === 'ci') return ciShort(waiting.ci);
  const [first, ...rest] = waiting.items;
  if (!first) return 'Waiting';
  const more = rest.length > 0 ? ` +${rest.length}` : '';
  return waiting.kind === 'turn' ? `Waits for ${first} to land${more}` : `Waits for ${first}${more}`;
}

/** A CI wait's line: "CI failed: task web:e2e (3/12)" after a failure, else "Waiting for CI · 9 of 14". */
function ciShort(ci: ItemWaiting['ci']): string {
  if (ci?.failure) {
    const [check] = ci.failure.checks;
    if (!check) return 'CI timed out';
    const job = check.failed_jobs[0];
    return job ? `CI failed: ${check.name} (${job})` : `CI failed: ${check.name}`;
  }
  return ci && ci.total > 0 ? `Waiting for CI · ${ci.finished} of ${ci.total}` : 'Waiting for CI';
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
