// "Spend this week" on Home (ADR 0016, wave 2): the week's spend against the summed budgets, a bar per
// factory and the footnote. The window is the last 7 UTC days, not a calendar week, so the footnote says
// "Last 7 days" (the picture's "Resets Monday" holds only for the org spend endpoint, which has no client).
// "Spend and billing" is not built yet: it renders through NotAvailable and never navigates.
import type { FactoryOverviewList, OrgSummary } from '../../api/types';
import { formatUsd } from '../../shared/format';
import { Meter } from '../../ui/hatch';
import { NotAvailable } from '../../ui/not-available';

export function SpendWeek({
  overview,
  mark,
}: {
  org: OrgSummary;
  overview: FactoryOverviewList;
  mark: (factory: { name: string }) => string;
}) {
  const budgets = overview.factories.flatMap((o) => (o.budget_usd === null ? [] : [o.budget_usd]));
  const budget = budgets.length > 0 ? budgets.reduce((sum, b) => sum + b, 0) : null;

  return (
    <section
      aria-labelledby="spend-week"
      className="flex min-w-0 flex-col rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 pb-2">
        <h2 id="spend-week" className="m-0 text-heading">
          Spend this week
        </h2>
        <p className="m-0 font-numeral text-heading font-semibold tabular-nums">
          {formatUsd(overview.spend_usd)}
          {budget !== null ? (
            <span className="text-secondary font-normal text-muted"> of {formatUsd(budget)}</span>
          ) : null}
        </p>
      </div>

      <ul className="m-0 list-none p-0">
        {overview.factories.map((o) => (
          <li key={o.factory.id} className="flex flex-col gap-2 border-t border-line py-3">
            <div className="flex items-center justify-between gap-3 text-secondary">
              <span className="flex min-w-0 items-center gap-2">
                <span
                  aria-hidden="true"
                  className="flex size-5 shrink-0 items-center justify-center rounded-chip bg-run font-numeral text-meta font-semibold text-white"
                >
                  {mark(o.factory)}
                </span>
                <span className="truncate">{o.factory.name}</span>
              </span>
              <span className="shrink-0 tabular-nums">
                {formatUsd(o.spend_usd)}
                {o.budget_usd !== null ? ` of ${formatUsd(o.budget_usd)}` : ' (no budget set)'}
              </span>
            </div>
            {o.budget_usd !== null ? (
              <Meter
                value={o.spend_usd}
                max={o.budget_usd}
                tone={o.spend_usd > o.budget_usd ? 'rework' : 'run'}
                label={`${o.factory.name}: ${formatUsd(o.spend_usd)} of ${formatUsd(o.budget_usd)}`}
                className="h-1.5 bg-panel"
              />
            ) : null}
          </li>
        ))}
      </ul>

      <div className="flex items-center justify-between gap-3 border-t border-line pt-3 text-meta text-muted">
        <span>Last 7 days</span>
        <NotAvailable label="Spend and billing">
          <button type="button" className="font-control text-run-ink underline">
            Spend and billing
          </button>
        </NotAvailable>
      </div>
    </section>
  );
}
