// Judges' words and numbers, shared by the list and the judge page: the summary line, the sample and
// pass marks, what a judge's runs read, and why a save or a try was refused (the publish's 409s).
import { ApiError } from '../../api/client';
import type { JudgeOutcome, JudgeScore, JudgeSummary } from '../../api/types';
import { formatPercent, formatUsd } from '../../shared/format';

export const none = '—';

/** "Scored 46 runs in 30 days, 81% passed, $3.20" (the judge page's summary line). */
export function summaryLine(summary: JudgeSummary): string {
  return `Scored ${summary.scored} runs in 30 days, ${formatPercent(summary.pass_rate)} passed, ${formatUsd(summary.cost_usd)}`;
}

/** The sample as a whole percent: 0.25 reads "25%". */
export function sampleText(sample: number): string {
  return `${Math.round(sample * 100)}%`;
}

/** A pass mark on the scores' 0 to 1 scale, to one decimal: 0.7. */
export function passMarkText(passMark: number): string {
  return passMark.toFixed(1);
}

/** An outcome passes when its score reaches the pass mark. */
export function outcomePasses(outcome: Pick<JudgeOutcome, 'score'>, passMark: number): boolean {
  return outcome.score >= passMark;
}

const seesWords: Record<string, string> = {
  events: "the run's events",
  spec: 'spec',
  pull_request: 'pull request',
};

/** What a judge run reads of the run it scores, in words: "The run's events, spec and pull request". */
export function seesText(sees: readonly string[]): string {
  const words = sees.map((s) => seesWords[s] ?? s);
  if (words.length === 0) return none;
  const list = words.length === 1 ? words[0] : `${words.slice(0, -1).join(', ')} and ${words.at(-1)}`;
  return `${list?.charAt(0).toUpperCase()}${list?.slice(1)}`;
}

/** The newest scores' outcome, or what became of a score that is not scored yet. */
export function scoreText(score: JudgeScore): string {
  switch (score.state) {
    case 'queued':
      return 'Queued';
    case 'running':
      return 'Running';
    case 'error':
      return 'Error';
    case 'scored':
      return score.outcome ?? 'Scored';
  }
}

/** The run's number from its ref: the text from its hash on. */
export function itemNumber(ref: string): string {
  const at = ref.indexOf('#');
  return at === -1 ? ref : ref.slice(at);
}

/** Why saving a judge was refused, in the page's words (the publish's 409s and the judge's 404). */
export function publishFailure(err: unknown): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Nothing was saved; try again once it is.';
  switch (err.code) {
    case 'draft-open':
      return 'The factory has an open draft. Publish or discard it first; nothing was saved.';
    case 'definition-invalid':
      return `The judge does not pass the definition's checks: ${err.detail ?? err.title}. Nothing was saved.`;
    case 'draft-base-moved':
      return 'A newer version is running. Reload the judge to see it, then save again.';
    case 'evals-pending':
      return 'The judge is in a draft that waits for an eval run to finish. It publishes once that run passes.';
    case 'evals-failing':
      return 'An eval run failed, so the draft stays open. Fix the case that failed, then publish the draft.';
    case 'judge-exists':
      return 'The current version has a judge with this name. Choose another name.';
    case 'judge-not-found':
      return 'This judge is not in the current version.';
    case 'judges-unavailable':
      return 'Judges are not available on this goald. Nothing was saved.';
    default:
      return `Not saved: ${err.detail ?? err.title}`;
  }
}

/** Why a try was refused, in the page's words. */
export function tryFailure(err: unknown): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Nothing was queued; try again once it is.';
  if (err.code === 'judges-unavailable') return 'Not queued: judges are not available on this goald.';
  return `Not queued: ${err.detail ?? err.title}`;
}

/** Why the list's switch could not publish the flip, in the table's words. */
export function toggleFailure(err: unknown): string {
  return `Not changed. ${publishFailure(err)}`;
}
