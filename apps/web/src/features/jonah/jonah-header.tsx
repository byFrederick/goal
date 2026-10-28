// Jonah's header (MASTER.md "Jonah"): the navy mark, the name and the screen's intro, then the tabs
// Overview, Proposals, Judges, Evals and Bake-offs. The tab the page is on is current. A tab with no
// page yet renders through NotAvailable, so it stays in the tab order and says "Not available yet".
import { Link } from '@tanstack/react-router';
import { SearchCheck } from 'lucide-react';
import { useFactoryParams } from '../../shared/factory';
import { cn } from '../../ui/cn';
import { NotAvailable } from '../../ui/not-available';

export type JonahTab = 'overview' | 'proposals' | 'judges' | 'evals' | 'bake-offs';

const tabLabels: Record<JonahTab, string> = {
  overview: 'Overview',
  proposals: 'Proposals',
  judges: 'Judges',
  evals: 'Evals',
  'bake-offs': 'Bake-offs',
};

/** The tabs in order; only Evals has a page so far. */
const tabOrder: readonly JonahTab[] = ['overview', 'proposals', 'judges', 'evals', 'bake-offs'];

const tabClass =
  '-mb-px inline-flex h-10 shrink-0 items-center gap-2 border-b-2 px-3 text-body transition-colors duration-200';

export function JonahHeader({ tab, caseCount }: { tab: JonahTab; caseCount?: number | undefined }) {
  const params = useFactoryParams();
  return (
    <div className="flex flex-col gap-4">
      <header className="flex flex-wrap items-center gap-3">
        <span aria-hidden="true" className="grid size-11 shrink-0 place-items-center rounded-card bg-andon text-white">
          <SearchCheck size={24} strokeWidth={2} />
        </span>
        <div className="flex min-w-0 flex-col gap-1">
          <h1 className="m-0 text-page-title tracking-display">Jonah</h1>
          <p className="m-0 text-body text-ink-soft">
            Eval cases replay a task an agent must get right, before any change to the definition is published.
          </p>
        </div>
      </header>

      <nav aria-label="Jonah" className="flex gap-1 overflow-x-auto border-b border-line">
        {tabOrder.map((t) => {
          const current = t === tab;
          const classes = cn(
            tabClass,
            current ? 'border-run font-control text-ink' : 'border-transparent text-ink-soft hover:text-ink',
          );
          if (t !== 'evals') {
            return (
              <NotAvailable key={t} label={tabLabels[t]}>
                <button type="button" className={cn(classes, 'cursor-not-allowed')}>
                  {tabLabels[t]}
                </button>
              </NotAvailable>
            );
          }
          return (
            <Link
              key={t}
              to="/o/$org/f/$factory/jonah/evals"
              params={params}
              aria-current={current ? 'page' : undefined}
              className={classes}
            >
              {tabLabels[t]}
              {caseCount === undefined ? null : (
                <span className="ml-2 text-meta font-normal text-muted tabular-nums">{caseCount}</span>
              )}
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
