import type { Metrics } from '../../api/types';
import { formatUsdCompact } from '../../shared/format';
import { type Stat, StatStrip } from '../../ui/stat-strip';

// The four flow figures of the Floor, as one stat strip (MASTER.md, Components: Stat strip): throughput, lead time
// p85, sent back and cost per item. Throughput, lead time and sent back come from the range's metrics against the
// previous range and the factory's targets. Cost per item is the spend over the finished items. Hands-off and
// spend today are not in the strip: the Floor shows four figures, as floor.jpg does.
export function FlowStats({ metrics }: { metrics: Metrics }) {
  const { targets, previous } = metrics;
  const noItems = metrics.items_done === 0;

  const throughput = metrics.throughput_per_day;
  const throughputTarget = targets.throughput_per_day;
  const noItemsEither = noItems && (previous?.items_done ?? 0) === 0;
  const throughputDelta = previous && !noItemsEither ? round1(throughput - previous.throughput_per_day) : null;

  const p85 = noItems ? null : (metrics.lead_time_hours?.p85 ?? null);
  const p85Target = targets.lead_time_hours;
  const p85Delta = p85 !== null && previous?.lead_time_hours ? Math.round(p85 - previous.lead_time_hours.p85) : null;

  const sentBack = Math.round(metrics.rework_rate * 100);
  const sentBackDelta = previous && !noItems ? Math.round((metrics.rework_rate - previous.rework_rate) * 100) : null;

  const perItem = noItems ? null : metrics.spend_usd / metrics.items_done;

  const stats: Stat[] = [
    {
      label: 'Throughput',
      value: throughput.toFixed(1),
      unit: 'a day',
      target: throughputTarget ? `Target ${throughputTarget}` : undefined,
      progress: throughputTarget ? throughput / throughputTarget : undefined,
      delta:
        throughputDelta !== null
          ? { text: signed(throughputDelta), good: goodWhen(throughputDelta > 0, throughputDelta) }
          : undefined,
      note: throughputTarget ? undefined : 'No target set',
    },
    {
      label: 'Lead time, p85',
      value: p85 === null ? null : Math.round(p85),
      unit: p85 === null ? undefined : 'h',
      target: p85Target ? `Target ${p85Target} h` : undefined,
      progress: p85 !== null && p85Target ? p85 / p85Target : undefined,
      delta: p85Delta !== null ? { text: `${signed(p85Delta)} h`, good: goodWhen(p85Delta < 0, p85Delta) } : undefined,
      note: p85Target ? undefined : 'No target set',
    },
    {
      label: 'Sent back',
      value: noItems ? null : sentBack,
      unit: noItems ? undefined : '%',
      note: 'No target set',
      delta:
        sentBackDelta !== null
          ? { text: `${signed(sentBackDelta)} pts`, good: goodWhen(sentBackDelta < 0, sentBackDelta) }
          : undefined,
    },
    {
      label: 'Cost per item',
      value: perItem === null ? null : formatUsdCompact(perItem),
      note: 'Per finished item',
    },
  ];

  return <StatStrip label="Flow against targets" stats={stats} />;
}

/** Whether a change is good, or undefined (neutral) when it is 0: nothing got better or worse. */
function goodWhen(good: boolean, delta: number): boolean | undefined {
  return delta === 0 ? undefined : good;
}

/** A change with its sign: "+2", "−0.6". Zero has no sign. */
function signed(n: number): string {
  if (n === 0) return '0';
  return `${n > 0 ? '+' : '−'}${Math.abs(n)}`;
}

function round1(n: number): number {
  return Math.round(n * 10) / 10;
}
