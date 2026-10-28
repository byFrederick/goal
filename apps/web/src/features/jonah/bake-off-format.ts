// What a bake-off screen says and how it is drawn, as pure functions: the names of its models, the colour of
// each arm, the banner's comparison, the subline and the scales of the pass-rate plot. The copy follows the
// bake-off picture (docs/design/screens/jonah/bake-off.html); the rules for the cases the picture does not show
// are here, so the page never builds a sentence of its own.
import type { BakeOff, BakeOffArm } from '../../api/types';
import { formatDay, formatPercent, formatUsd, formatUsdCompact } from '../../shared/format';

const clock = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit' });

/** "claude-haiku-5-5" is "haiku 5.5" on the plot's labels. An id of another shape keeps its whole name. */
export function shortModel(model: string): string {
  const m = /^claude-([a-z]+)-(\d+)-(\d+)$/.exec(model);
  return m ? `${m[1]} ${m[2]}.${m[3]}` : model;
}

/** "claude-haiku-5-5" is "haiku": the family the per-case grid's columns are named by. */
export function modelFamily(model: string): string {
  const m = /^claude-([a-z]+)/.exec(model);
  return m?.[1] ?? model;
}

/**
 * The dot colour of an arm, by its number: 1 ok, 2 run-soft, 3 run, and a fourth arm muted. The same
 * colour marks the arm on the plot, in the table and in the grid's header, so an arm reads one way.
 */
const armDots = ['bg-ok', 'bg-run-soft', 'bg-run', 'bg-muted'] as const;

export function armDot(arm: number): string {
  return armDots[arm - 1] ?? 'bg-muted';
}

/**
 * A per-case cell's shade, by the tries that passed of the tries run: all of them is the full shade, two or
 * more is partial, and 0 or 1 is a failing shade. The count is in the cell too, so colour is not the only signal.
 */
export function cellShade(passed: number, tries: number): string {
  if (passed === tries) return 'bg-ok-ink text-shell';
  if (passed >= 2) return 'bg-ok-tint text-ok-ink';
  return 'bg-rework-tint text-rework-ink';
}

/** The arm the bake-off runs the agent's model on, which the banner compares the recommendation with. */
export function currentArm(bakeOff: BakeOff): BakeOffArm | undefined {
  return bakeOff.arms.find((a) => a.is_current);
}

/** The banner's words: a headline and the sentence under it. */
export interface Recommendation {
  headline: string;
  sentence: string;
}

/** How many times the current cost the recommended one is, as the picture says it: "a tenth of the cost". */
const fractions: Record<number, string> = { 2: 'half', 3: 'a third', 4: 'a quarter', 5: 'a fifth', 10: 'a tenth' };

function costPhrase(recCents: number, curCents: number): string {
  if (recCents === curCents) return 'the same cost';
  if (recCents > curCents) return `${Math.round((recCents / curCents) * 10) / 10} times the cost`;
  if (recCents === 0) return 'no cost';
  const ratio = Math.round(curCents / recCents);
  const word = fractions[ratio];
  return word ? `${word} of the cost` : `${Math.round((recCents / curCents) * 100)}% of the cost`;
}

/**
 * The banner for a done bake-off whose recommended arm is not the current model; none otherwise. The picture's
 * sentence is "93% of tries passed, like claude-opus-5-5, for $0.24 an item instead of $2.40." When the pass
 * rates differ, the sentence names both rates and the current model instead of "like".
 */
export function recommendationOf(bakeOff: BakeOff): Recommendation | undefined {
  if (bakeOff.recommended_arm === null) return undefined;
  const rec = bakeOff.arms.find((a) => a.arm === bakeOff.recommended_arm);
  const cur = currentArm(bakeOff);
  if (!rec || !cur || rec.arm === cur.arm || !rec.summary || !cur.summary) return undefined;

  const recPct = Math.round(rec.summary.pass_rate * 100);
  const curPct = Math.round(cur.summary.pass_rate * 100);
  const recCost = rec.summary.cost_per_item_usd;
  const curCost = cur.summary.cost_per_item_usd;
  const cost = costPhrase(Math.round(recCost * 100), Math.round(curCost * 100));

  const passWords =
    recPct === curPct ? 'the same pass rate' : recPct > curPct ? 'a higher pass rate' : 'a lower pass rate';
  const headline = `${rec.model}: ${passWords} at ${cost}`;

  const sentence =
    recPct === curPct
      ? `${recPct}% of tries passed, like ${cur.model}, for ${formatUsd(recCost)} an item instead of ${formatUsd(curCost)}.`
      : `${recPct}% of tries passed against ${curPct}% for ${cur.model}, at ${formatUsd(recCost)} an item instead of ${formatUsd(curCost)}.`;
  return { headline, sentence };
}

/** The status words a bake-off that is not done shows in its subline, in MASTER.md's chip words. */
export const statusWords: Record<BakeOff['state'], string> = {
  queued: 'Queued',
  running: 'Running',
  done: 'Finished',
  failed: 'Failed',
  cancelled: 'Cancelled',
};

/** "Oct 25", the day a bake-off was made, for the breadcrumb and the subline. */
export function bakeOffDay(bakeOff: BakeOff): string {
  return formatDay(bakeOff.created_at);
}

/** The breadcrumb's last crumb: "frontend-engineer, Oct 25". */
export function bakeOffCrumb(bakeOff: BakeOff): string {
  return `${bakeOff.agent}, ${bakeOffDay(bakeOff)}`;
}

/** The title: "Which model for frontend-engineer?". */
export function bakeOffTitle(bakeOff: BakeOff): string {
  return `Which model for ${bakeOff.agent}?`;
}

/** The part of the subline before the finish or the status: "3 models on the same 5 frontend cases, 3 tries each." */
export function bakeOffScope(bakeOff: BakeOff): string {
  const models = bakeOff.arms.length;
  return `${models} models on the same ${bakeOff.cases} ${bakeOff.station} cases, ${bakeOff.trials} tries each.`;
}

/** "Finished Oct 25 at 18:40" for a done bake-off; the status words are the page's chip when it is not done. */
export function finishedAt(bakeOff: BakeOff): string | undefined {
  if (bakeOff.finished_at === undefined) return undefined;
  const finished = new Date(bakeOff.finished_at);
  return `Finished ${formatDay(bakeOff.finished_at)} at ${clock.format(finished)}`;
}

/** The cost the bake-off has spent or will spend, in the subline: "$14.20". */
export function bakeOffCost(bakeOff: BakeOff): string {
  return formatUsd(bakeOff.cost_usd);
}

/** The pass rate of an arm, or undefined while its tries have not finished. */
export function passRateOf(arm: BakeOffArm): number | undefined {
  return arm.summary?.pass_rate;
}

/** A judged share as a percentage: "95%". */
export function judgedOf(share: number): string {
  return formatPercent(share);
}

/** The column that names a judge's share: "Judged plain language" for the judge titled "Plain language". */
export function judgedHeading(title: string): string {
  return `Judged ${title.charAt(0).toLowerCase()}${title.slice(1)}`;
}

/** Whole dollars for the plot's cost ticks: "$0", "$1". */
export function costTick(usd: number): string {
  return formatUsdCompact(usd);
}
