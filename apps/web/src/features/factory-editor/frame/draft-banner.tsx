// The draft banner (screens build/definition): the draft's version and change count, the problem
// sentence ("Fix 1 problem before you publish. Running items keep v21."), and the actions the
// frame passes in (Review changes, Discard draft). Rendered only while there is a draft.
import type { ReactNode } from 'react';
import { plural } from '../../../shared/format';

interface DraftBannerProps {
  /** The number the draft publishes as. */
  next: number;
  /** The running version's number; null before the first publish. */
  current: number | null;
  changeCount: number;
  problemCount: number;
  children: ReactNode;
}

export function DraftBanner({ next, current, changeCount, problemCount, children }: DraftBannerProps) {
  const problems = problemCount > 0 ? `Fix ${plural(problemCount, 'problem')} before you publish.` : '';
  const running = current !== null ? `Running items keep v${current}.` : '';
  return (
    <section
      aria-label="Draft"
      className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-card bg-run-tint px-4 py-3"
    >
      <p className="m-0 text-secondary">
        <span className="font-control text-run-ink">
          Draft of v{next}, {plural(changeCount, 'change')}
        </span>{' '}
        <span className="text-ink-soft">
          {problems} {running}
        </span>
      </p>
      <div className="ml-auto flex flex-wrap items-center gap-2">{children}</div>
    </section>
  );
}
