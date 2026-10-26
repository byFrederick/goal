import { Link } from '@tanstack/react-router';
import { ChevronRight } from 'lucide-react';
import type { Factory, LeadTimeBridge } from '../../api/types';
import { useFactoryParams } from '../../shared/factory';
import { formatHours, plural } from '../../shared/format';
import { Bridge, type BridgeRow } from '../../ui/bridge';
import { SectionCard } from '../../ui/card';
import { bridgeInsight, stepLabel } from './bridge-words';

/** An axis tick, in hours to one decimal so the scale reads evenly: "6.0 h". */
const tick = (h: number) => `${h.toFixed(1)} h`;
/** A target as it was set: "6 h", "6.5 h". */
const targetHours = (h: number) => `${Number.isInteger(h) ? h : h.toFixed(1)} h`;

/**
 * Why the p85 lead time is over its target (Main: the bridge): from the target, one step per
 * kind of time that explains the gap, to the actual; the insight names the largest step and the
 * constraint. Other states say the line is on target, has no target, or has too few items.
 */
export function LeadTimeCard({
  bridge,
  factory,
  className,
}: {
  bridge: LeadTimeBridge;
  factory: Factory;
  className?: string;
}) {
  const params = useFactoryParams();
  const flow = (
    <Link
      to="/o/$org/f/$factory/board"
      params={params}
      className="inline-flex min-h-6 items-center gap-1 text-sm whitespace-nowrap no-underline hover:text-run"
    >
      Flow
      <ChevronRight size={14} strokeWidth={1.75} aria-hidden="true" />
    </Link>
  );
  const { target_hours: target, actual_p85_hours: actual, gap_hours: gap } = bridge;

  if (bridge.status !== 'over_target' || target === null || actual === null || gap === null) {
    return (
      <SectionCard title="Lead time against target" aside={flow} className={className}>
        <p className="m-0 text-[15px] leading-normal">{quietSentence(bridge)}</p>
      </SectionCard>
    );
  }

  const largest = bridge.steps[0];
  const insight = largest ? bridgeInsight(largest, bridge.constraint, factory.stations) : null;
  const rows: BridgeRow[] = [
    { key: 'target', label: 'Target', text: formatHours(target) },
    ...bridge.steps.map((s) => ({
      key: `${s.component}:${s.station ?? ''}`,
      label: stepLabel(s, factory.stations),
      text: `+${formatHours(s.hours)}${s.items > 0 ? `, ${plural(s.items, 'item')}` : ''}`,
    })),
    { key: 'actual', label: 'Actual', text: formatHours(actual) },
  ];
  const summary = `Lead time p85 ${formatHours(actual)} against a target of ${targetHours(target)}: ${bridge.steps
    .map((s) => `${stepLabel(s, factory.stations)} +${formatHours(s.hours)}`)
    .join(', ')}.`;

  return (
    <SectionCard
      title="Why lead time is over target"
      description={`+${formatHours(gap)} over the ${targetHours(target)} target, p85`}
      aside={flow}
      className={className}
    >
      <Bridge
        input={{ target, steps: bridge.steps.map((s, i) => ({ key: rows[i + 1]?.key ?? '', value: s.hours })), actual }}
        rows={rows}
        formatTick={tick}
        summary={summary}
        caption="Lead time from target to actual, p85"
        columns={['Step', 'Time']}
        className="mt-4"
      />
      {insight ? (
        <div className="mt-auto rounded-control border border-line bg-surface px-4 py-3.5">
          <p className="m-0 text-[15px] leading-snug">{insight.lead}</p>
          {insight.constraint ? <p className="m-0 mt-1.5 text-sm text-muted">{insight.constraint}</p> : null}
        </div>
      ) : null}
    </SectionCard>
  );
}

/** What the card says when there is no gap to explain. */
function quietSentence(b: LeadTimeBridge): string {
  if (b.status === 'no_target' || b.target_hours === null) {
    return 'No lead-time target is set. Set one in the definition (Line, flow targets) to see what holds lead time back.';
  }
  if (b.status === 'not_enough_data' || b.actual_p85_hours === null) {
    return `Not enough items finished in this range: the bridge needs 5, ${plural(b.items_done, 'item')} finished.`;
  }
  const under = b.target_hours - b.actual_p85_hours;
  return under < 0.05
    ? `Lead time is on the ${targetHours(b.target_hours)} target, p85.`
    : `Lead time is ${formatHours(under)} under the ${targetHours(b.target_hours)} target, p85.`;
}
