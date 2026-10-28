// The Budget card of the dark panel (docs/design/screens/get-started/budget.jpg): a half-circle gauge of the
// day's budget out of the largest day, then the day and the week in words. The arc is the andon panel's
// tokens; its colour is run-soft, not amber, because MASTER.md keeps amber for the constraint.
import { formatUsdCompact } from '../../shared/format';
import { gaugeFraction } from './budget-hints';

/** The arc's length: half a circle of radius 45, the picture's drawing. */
const arcLength = Math.PI * 45;

export function BudgetCard({ day, week }: { day: number; week: number }) {
  const sentence = `${formatUsdCompact(day)} a day, ${formatUsdCompact(week)} a week`;
  return (
    <section
      aria-label="Budget"
      className="flex min-w-0 flex-col gap-2 rounded-card border border-andon-line bg-andon-raised p-4"
    >
      <span className="text-meta text-andon-text">Budget</span>
      <svg width="110" height="62" viewBox="0 0 110 62" aria-hidden="true" className="block h-auto w-full">
        <path
          d="M10 56a45 45 0 0 1 90 0"
          fill="none"
          strokeWidth={10}
          strokeLinecap="round"
          className="stroke-andon-line"
        />
        <path
          d="M10 56a45 45 0 0 1 90 0"
          fill="none"
          strokeWidth={10}
          strokeLinecap="round"
          className="stroke-run-soft"
          strokeDasharray={`${gaugeFraction(day) * arcLength} ${arcLength * 2}`}
        />
      </svg>
      <span className="font-control text-heading break-words text-white">{sentence}</span>
    </section>
  );
}
