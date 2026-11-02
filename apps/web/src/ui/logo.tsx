import { GoalMark } from './goal-mark';

/** The Goal mark and the name, top left of the sign-in pages (sign-up.html's brand). */
export function Wordmark() {
  return (
    <span className="flex items-center gap-3 text-detail-title tracking-display text-ink">
      <GoalMark size="brand" />
      Goal
    </span>
  );
}
