import { useQuery } from '@tanstack/react-query';
import { overviewQuery } from '../../api/queries';
import type { Factory, Metrics } from '../../api/types';
import { formatUsdCompact } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { type KpiBar, KpiTile } from '../../ui/kpi-tile';
import { NotAvailable } from '../../ui/not-available';

// The six flow figures of the Floor, in one row (MASTER.md, Components: KPI tile). Throughput, lead
// time and sent back come from the range's metrics against the previous range and the factory's
// targets. Spend comes from the overview, one day and seven days. Hands-off is not built yet, so
// its tile is disabled rather than given a number (ADR 0016, decision 4). A phone shows four.
export function KpiRow({ factory, metrics, className }: { factory: Factory; metrics: Metrics; className?: string }) {
  const { targets, previous } = metrics;
  const noItems = metrics.items_done === 0;

  const throughput = metrics.throughput_per_day;
  const throughputTarget = targets.throughput_per_day;
  const throughputDelta = previous ? round1(throughput - previous.throughput_per_day) : null;

  const p85 = noItems ? null : (metrics.lead_time_hours?.p85 ?? null);
  const p85Target = targets.lead_time_hours;
  const p85Delta = p85 !== null && previous?.lead_time_hours ? Math.round(p85 - previous.lead_time_hours.p85) : null;

  const sentBack = Math.round(metrics.rework_rate * 100);
  const sentBackDelta = previous && !noItems ? Math.round((metrics.rework_rate - previous.rework_rate) * 100) : null;

  const perItem = noItems ? null : metrics.spend_usd / metrics.items_done;

  return (
    <section
      aria-label="Flow against targets"
      className={cn('grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6', className)}
    >
      <KpiTile
        label="Throughput"
        value={throughput.toFixed(1)}
        unit="a day"
        bar={
          throughputTarget
            ? meter(throughput, throughputTarget, 'run', `the target of ${throughputTarget} a day`)
            : undefined
        }
        sparkline={
          !throughputTarget && metrics.throughput.length > 1
            ? { points: metrics.throughput.map((d) => d.done), label: 'Items finished per day' }
            : undefined
        }
        target={throughputTarget ? `Target ${throughputTarget}` : undefined}
        delta={throughputDelta !== null ? { value: throughputDelta, good: throughputDelta > 0 } : undefined}
      />

      <KpiTile
        label="Lead time, p85"
        value={p85 === null ? notEnough : Math.round(p85)}
        unit={p85 === null ? undefined : 'h'}
        bar={p85 !== null && p85Target ? meter(p85, p85Target, 'ok', `the ${p85Target} hour target`) : undefined}
        target={p85Target ? `Target ${p85Target} h` : undefined}
        delta={p85Delta !== null ? { value: p85Delta, unit: 'h', good: p85Delta < 0 } : undefined}
      />

      <KpiTile
        label="Sent back"
        value={noItems ? notEnough : sentBack}
        unit={noItems ? undefined : '%'}
        target="No target set"
        delta={sentBackDelta !== null ? { value: sentBackDelta, unit: 'pts', good: sentBackDelta <= 0 } : undefined}
      />

      {/* Not built yet: disabled with a tip, never a number (ADR 0016, decision 4). */}
      <NotAvailable>
        <button type="button" aria-label="Hands-off" className="hidden min-w-0 rounded-card text-left md:flex">
          <KpiTile
            label="Hands-off"
            value={<span className="text-body text-ink-soft">Not built yet</span>}
            target="No human touch"
            className="flex-1 opacity-60"
          />
        </button>
      </NotAvailable>

      <div className="hidden min-w-0 md:flex">
        <KpiTile
          label="Cost per item"
          value={perItem === null ? notEnough : formatUsdCompact(perItem)}
          target="Per finished item"
          className="flex-1"
        />
      </div>

      <SpendToday factory={factory} />
    </section>
  );
}

/** Today's spend against the factory's budget for the day, with the week's spend beside it. */
function SpendToday({ factory }: { factory: Factory }) {
  const org = useOrg();
  const today = useQuery(overviewQuery(org.id, '1d'));
  const week = useQuery(overviewQuery(org.id, '7d'));

  if (today.isPending) {
    return (
      <div aria-busy="true" className="min-w-0">
        <KpiTile label="Spend today" value={<span className="sr-only">Loading</span>} />
      </div>
    );
  }
  if (today.isError) {
    return (
      <KpiTile
        label="Spend today"
        value={<span className="text-body text-ink-soft">Could not load</span>}
        note={
          <Button size="sm" variant="ghost" onClick={() => void today.refetch()}>
            Retry
          </Button>
        }
      />
    );
  }

  const row = today.data.factories.find((f) => f.factory.id === factory.id);
  const weekRow = week.data?.factories.find((f) => f.factory.id === factory.id);
  if (!row) return <KpiTile label="Spend today" value={notEnough} />;

  const { spend_usd: spend, budget_usd: budget } = row;
  return (
    <KpiTile
      label="Spend today"
      value={formatUsdCompact(spend)}
      unit={budget ? `of ${formatUsdCompact(budget)}` : undefined}
      bar={budget ? meter(spend, budget, 'amber', "today's budget") : undefined}
      target={weekRow ? `Week ${formatUsdCompact(weekRow.spend_usd)}` : undefined}
    />
  );
}

/** A bar against a target: the meter's name gives the share, "76% of the target of 10 a day". */
function meter(value: number, max: number, tone: KpiBar['tone'], of: string): KpiBar {
  const pct = Math.round((value / max) * 100);
  return { value, max, tone, label: `${pct}% of ${of}` };
}

/** What a figure shows when its range has nothing to measure. */
const notEnough = <span className="text-body text-ink-soft">Not enough data</span>;

function round1(n: number): number {
  return Math.round(n * 10) / 10;
}
