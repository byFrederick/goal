// Why a queued or ready item waits on another item, in words. The board card gets one short line
// (waitingShort) and its tooltip the full list (waitingDetail); the item page lists each need itself.
import type { CICheck, ItemWaiting } from '../api/types';

/** A commit's sha as people read it: its first 7 characters. */
export const shortSha = (sha: string): string => sha.slice(0, 7);

/** The checks of one repository that have finished, and all of them: the "9 of 14" of a commit. */
function repoCounts(checks: readonly CICheck[], repo: string): { finished: number; total: number } {
  const own = checks.filter((c) => c.repo === repo);
  return { finished: own.filter((c) => c.state === 'finished').length, total: own.length };
}

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

/**
 * A CI wait on a narrow surface (a Board card, a Plan row): its lead and, after a failure, the check
 * that failed, so only the check's name has to truncate. The long form is waitingShort.
 */
export function ciCard(ci: ItemWaiting['ci']): { lead: string; check?: string } {
  if (ci?.failure) {
    const [check] = ci.failure.checks;
    return check ? { lead: 'CI failed:', check: check.name } : { lead: 'CI timed out' };
  }
  return { lead: ci && ci.total > 0 ? `CI · ${ci.finished} of ${ci.total}` : 'CI · waiting' };
}

/** The card's CI wording as one string: "CI · 9 of 14", "CI · waiting", "CI failed: task web:e2e". */
export function waitingCard(waiting: Pick<ItemWaiting, 'ci'>): string {
  const { lead, check } = ciCard(waiting.ci);
  return check ? `${lead} ${check}` : lead;
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
  if (waiting.kind === 'ci') {
    const ci = waiting.ci;
    if (!ci) return 'Waiting for CI';
    const lines = ci.commits.map((c) => {
      const { finished, total } = repoCounts(ci.checks, c.repo);
      return `${c.repo} ${shortSha(c.sha)} · ${total > 0 ? `${finished} of ${total}` : 'no checks yet'}`;
    });
    for (const check of ci.failure?.checks ?? []) {
      const jobs = check.failed_jobs.length > 0 ? ` (${check.failed_jobs.join(', ')})` : '';
      lines.push(`Failed: ${check.name}${jobs}`);
    }
    return lines.join('\n');
  }
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
