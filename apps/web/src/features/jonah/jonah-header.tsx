// Jonah's header (MASTER.md "Jonah"): the navy mark, the name and the intro of the tab the page is on,
// then the tabs Overview, Proposals, Judges, Evals and Bake-offs. The tab the page is on is current, and
// each tab can show a count. A tab with no page yet renders through NotAvailable, so it stays in the tab
// order and says "Not available yet".
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

/** The tabs in order; Judges and Evals have pages so far. */
const tabOrder: readonly JonahTab[] = ['overview', 'proposals', 'judges', 'evals', 'bake-offs'];

const evalsIntro =
  'Eval cases replay a task an agent must get right, before any change to the definition is published.';

/** The intro line under the name, per tab. The tabs without a page keep the evals line, the first Jonah had. */
const introOf: Record<JonahTab, string> = {
  overview: evalsIntro,
  proposals: evalsIntro,
  judges: 'Judges read a sample of runs and score them against a rubric you write.',
  evals: evalsIntro,
  'bake-offs': evalsIntro,
};

const tabClass =
  '-mb-px inline-flex h-10 shrink-0 items-center gap-2 border-b-2 px-3 text-body transition-colors duration-200';

export function JonahHeader({ tab, counts = {} }: { tab: JonahTab; counts?: Partial<Record<JonahTab, number>> }) {
  const params = useFactoryParams();
  return (
    <div className="flex flex-col gap-4">
      <header className="flex flex-wrap items-center gap-3">
        <span aria-hidden="true" className="grid size-11 shrink-0 place-items-center rounded-card bg-andon text-white">
          <SearchCheck size={24} strokeWidth={2} />
        </span>
        <div className="flex min-w-0 flex-col gap-1">
          <h1 className="m-0 text-page-title tracking-display">Jonah</h1>
          <p className="m-0 text-body text-ink-soft">{introOf[tab]}</p>
        </div>
      </header>

      <nav aria-label="Jonah" className="flex gap-1 overflow-x-auto border-b border-line">
        {tabOrder.map((t) => {
          const current = t === tab;
          const classes = cn(
            tabClass,
            current ? 'border-run font-control text-ink' : 'border-transparent text-ink-soft hover:text-ink',
          );
          const count = counts[t];
          const label = (
            <>
              {tabLabels[t]}
              {count === undefined ? null : (
                <span className="ml-2 text-meta font-normal text-muted tabular-nums">{count}</span>
              )}
            </>
          );
          if (t === 'judges') {
            return (
              <Link
                key={t}
                to="/o/$org/f/$factory/jonah/judges"
                params={params}
                aria-current={current ? 'page' : undefined}
                className={classes}
              >
                {label}
              </Link>
            );
          }
          if (t !== 'evals') {
            return (
              <NotAvailable key={t} label={tabLabels[t]}>
                <button type="button" className={cn(classes, 'cursor-not-allowed')}>
                  {label}
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
              {label}
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
