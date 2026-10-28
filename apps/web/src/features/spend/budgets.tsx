// Budgets per factory: its daily and weekly caps (set in the factory's definition, so shown here, not
// edited), today's spend against the daily cap, and the state. A stopped factory has an open andon pull
// and its reason as the line under its bar. The three rules under the list are fixed copy: SpendBudget
// carries no per-item cap, so Per item says what a run does at its own cap.

import type { SpendBudget } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { Chip, type ChipTone } from '../../ui/chip';
import { meterOf } from './meter';
import { money, percentOf } from './view';

export function Budgets({ budgets }: { budgets: readonly SpendBudget[] }) {
  return (
    <section aria-labelledby="bud-h" className="flex min-w-0 flex-col rounded-card border border-line bg-card p-4">
      <div className="mb-2 flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 id="bud-h" className="m-0 text-heading text-ink">
          Budgets
        </h2>
        <span className="text-meta text-muted">Set in each factory's definition</span>
      </div>
      {budgets.length === 0 ? (
        <p className="m-0 py-3 text-secondary text-muted">No factory has a budget yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {budgets.map((b) => (
            <BudgetRow key={b.factory.id} budget={b} />
          ))}
        </ul>
      )}
      <dl className="m-0 flex flex-col gap-2 border-t border-line pt-4 pb-3 text-secondary">
        <Rule term="At 80%" detail="Owners and admins get a Needs you alert" />
        <Rule term="At 100%" detail="The factory drains: running items finish, nothing new starts until midnight UTC" />
        <Rule term="Per item" detail="A run stops at the item's cap and asks you" />
      </dl>
    </section>
  );
}

function Rule({ term, detail }: { term: string; detail: string }) {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:gap-4">
      <dt className="w-24 shrink-0 font-semibold text-ink">{term}</dt>
      <dd className="m-0 text-ink-soft">{detail}</dd>
    </div>
  );
}

function BudgetRow({ budget: b }: { budget: SpendBudget }) {
  const daily = b.daily_usd;
  const pct = daily === null ? null : percentOf(b.today_usd, daily);
  const stopped = b.state === 'stopped';
  const over = b.state === 'over_budget';
  const fill = stopped || over ? 'bg-rework' : pct !== null && pct >= 50 ? 'bg-amber' : 'bg-ok';
  const tone: ChipTone = stopped || over ? 'rework' : pct !== null && pct >= 50 ? 'constraint' : 'ok';
  const title = b.factory.title || b.factory.name;

  return (
    <li className="flex flex-col gap-3 border-t border-line py-3 md:flex-row md:items-center">
      <div className="flex w-full min-w-0 items-center gap-3 md:w-60 md:shrink-0">
        <FactoryBadge mark={b.factory.name.slice(0, 1).toUpperCase()} className="size-6" />
        <div className="flex min-w-0 flex-col leading-snug">
          <b className="font-semibold text-ink">{title}</b>
          <span className="text-meta text-muted">
            {money(daily)} a day, {money(b.weekly_usd)} a week
          </span>
        </div>
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        {pct === null ? null : (
          <div
            {...meterOf(stopped ? `${title}, line stopped` : `${pct}% of today`, stopped ? 0 : b.today_usd, daily ?? 0)}
            className="h-2 overflow-hidden rounded-chip bg-hover"
          >
            <span className={`block h-full rounded-chip ${fill}`} style={{ width: `${stopped ? 0 : pct}%` }} />
          </div>
        )}
        <span className="text-meta text-muted">
          {stopped
            ? (b.reason ?? 'Stopped')
            : `Today ${money(b.today_usd)}${daily === null ? '' : ` of ${money(daily)}`}`}
        </span>
        {b.weekly_usd === null ? null : (
          <span className="text-meta text-muted">
            Week {money(b.week_usd)} of {money(b.weekly_usd)}
          </span>
        )}
      </div>
      {stopped || over ? (
        <Chip tone="rework">{stopped ? 'Stopped' : 'Over budget'}</Chip>
      ) : pct === null ? null : (
        <Chip tone={tone}>{pct}% today</Chip>
      )}
    </li>
  );
}
