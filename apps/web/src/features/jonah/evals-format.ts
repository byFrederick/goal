// What Jonah's Evals screen says about its cases, in plain words (MASTER.md vocabulary). Pure, so the
// wording and the dots are tested without the page.
import { ApiError } from '../../api/client';
import type { EvalCaseEntry, EvalCheckedBy, EvalHistoryTry, EvalResultState } from '../../api/types';
import { formatClock } from '../../shared/format';

/** What a cell with no value shows: a dash, as the Agent page does. */
export const none = '—';

/** A try's dot: passed is green, failed and error are red, a try not finished yet is empty. */
export type TryTone = 'pass' | 'fail' | 'open';

export function tryTone(state: EvalResultState): TryTone {
  if (state === 'pass') return 'pass';
  if (state === 'fail' || state === 'error') return 'fail';
  return 'open';
}

/**
 * The dots of a case's last tries, oldest on the left so the newest is the one at the right. The API
 * gives them newest first.
 */
export function dotsOf(tries: readonly EvalHistoryTry[]): TryTone[] {
  return tries.map((t) => tryTone(t.state)).reverse();
}

/** "2 of 3 passed, last one failed": what the dots say, for a screen reader and a pointer's tip. */
export function triesLabel(tries: readonly EvalHistoryTry[]): string {
  if (tries.length === 0) return 'No finished try yet';
  const passed = tries.filter((t) => t.state === 'pass').length;
  const last = tryTone(tries[0]?.state ?? 'pending');
  const tail = last === 'fail' ? ', last one failed' : '';
  return `${passed} of ${tries.length} passed${tail}`;
}

/** "Today 09:12" for a run finished today, the date and time before. */
export function runStamp(iso: string, now = new Date()): string {
  const sameDay = new Date(iso).toDateString() === now.toDateString();
  return sameDay ? `Today ${formatClock(iso, now)}` : formatClock(iso, now);
}

/** Who checks a case, in words. The contract does not name the agent's contract, so it reads as the agent's. */
export function checkedByText(by: EvalCheckedBy): string {
  if (by.kind === 'judge') return `Judge: ${by.title ?? by.judge ?? 'judge'}`;
  if (by.kind === 'reference_patch') return 'Reference patch';
  return 'Agent contract';
}

/** The cases a summary's last run covered, in words for the tile's meta line. */
export function lastRunMeta(latestRunAt: string | undefined, triesEach: number | undefined, now = new Date()): string {
  if (latestRunAt === undefined) return 'No run yet';
  const tries = triesEach ?? 0;
  return `${runStamp(latestRunAt, now)}, ${tries} ${tries === 1 ? 'try' : 'tries'} each`;
}

/** The summary's share of tries that passed, in words: "13 of 15 tries". */
export function firstTryMeta(tried: number, passed: number): string {
  if (tried === 0) return 'No tries yet';
  return `${passed} of ${tried} tries`;
}

/** The cases' names, for the table's accessible labels. */
export function caseLabel(entry: EvalCaseEntry): string {
  return `Run ${entry.name}`;
}

/** Why starting a run failed, in the page's words. */
export function runFailure(err: unknown): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Nothing started; try again once it is.';
  if (err.code === 'evals-unavailable') return 'Not started: evals are not available on this goald.';
  return `Not started: ${err.detail ?? err.title}`;
}

/** Why adding a case was refused, in the dialog's words (the 409s of the publish's checks). */
export function addCaseFailure(err: unknown): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. The case is not added; try again once it is.';
  switch (err.code) {
    case 'eval-case-exists':
      return 'The current version already has a case with this name. Choose another name.';
    case 'draft-open':
      return 'The factory has an open draft. Publish or discard it first; nothing was added.';
    case 'evals-pending':
      return 'The case is in a draft that waits for an eval run to finish. It publishes once that run passes.';
    case 'evals-failing':
      return 'An eval run failed, so the draft stays open. Fix the case that failed, then publish the draft.';
    case 'evals-unavailable':
      return 'Evals are not available on this goald. Nothing was added.';
    default:
      return `Not added: ${err.detail ?? err.title}`;
  }
}

/** A case name: 3 to 40 lowercase letters, digits and hyphens, as the contract states it. */
export const caseNamePattern = /^[a-z0-9-]{3,40}$/;
