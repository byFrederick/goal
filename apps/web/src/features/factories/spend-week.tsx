// "Spend this week" on Home (ADR 0016, wave 2): the week's spend against the summed budgets, a bar per
// factory and the footer. The window is the last 7 UTC days, not a calendar week, so the footer says
// "Last 7 days" where the picture says "Resets Monday 00:00 UTC": that copy needs a calendar-week split
// per factory, which the API does not give. The bars use the factory badge's colour until factory colours
// ship. "Spend and billing" opens the organization's Spend page.
import { Link } from '@tanstack/react-router';
import type { FactoryOverviewList, OrgSummary } from '../../api/types';
import { formatUsd } from '../../shared/format';
import { Meter } from '../../ui/hatch';

export function SpendWeek({
  org,
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
      className="@container flex min-w-0 flex-col rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 pb-2">
        <h2 id="spend-week" className="m-0 text-heading">
          Spend this week
        </h2>
        <p className="m-0 text-heading font-semibold tabular-nums">
          {formatUsd(overview.spend_usd)}
          {budget !== null ? (
            <span className="text-secondary font-normal text-muted"> of {formatUsd(budget)}</span>
          ) : null}
        </p>
      </div>

      <ul className="m-0 list-none p-0">
        {overview.factories.map((o) => (
          <li
            key={o.factory.id}
            className="flex flex-wrap items-center gap-x-3 gap-y-2 border-t border-line py-3 text-secondary"
          >
            <span className="flex min-w-0 flex-1 items-center gap-2 @[480px]:w-40 @[480px]:flex-none">
              <span
                aria-hidden="true"
                className="flex size-5 shrink-0 items-center justify-center rounded-chip bg-run text-meta font-semibold text-white"
              >
                {mark(o.factory)}
              </span>
              <span className="truncate">{o.factory.name}</span>
            </span>
            <span className="order-2 shrink-0 tabular-nums @[480px]:order-3">
              {formatUsd(o.spend_usd)}
              {o.budget_usd !== null ? ` of ${formatUsd(o.budget_usd)}` : ' (no budget set)'}
            </span>
            {o.budget_usd !== null ? (
              <Meter
                value={o.spend_usd}
                max={o.budget_usd}
                variant="solid"
                tone={o.spend_usd > o.budget_usd ? 'rework' : 'run'}
                label={`${o.factory.name}: ${formatUsd(o.spend_usd)} of ${formatUsd(o.budget_usd)}`}
                className="order-3 h-1.5 w-full bg-line @[480px]:order-2 @[480px]:w-auto @[480px]:min-w-0 @[480px]:flex-1"
              />
            ) : null}
          </li>
        ))}
      </ul>

      <div className="flex items-center justify-between gap-3 border-t border-line pt-3 text-meta text-muted">
        <span>Last 7 days</span>
        <Link
          to="/o/$org/spend"
          params={{ org: org.slug }}
          className="font-control text-run-ink no-underline hover:underline"
        >
          Spend and billing
        </Link>
      </div>
    </section>
  );
}
