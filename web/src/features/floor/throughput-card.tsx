import type { Metrics } from '../../api/types';
import { formatHours, formatPercent, plural } from '../../shared/format';
import { SectionCard, StatRow } from '../../ui/card';
import { cn } from '../../ui/cn';
import { TargetBar } from '../../ui/target-bar';
import { type Delta, leadTimeDelta, longWaitSentence, reworkDelta, targetSentence, throughputDelta } from './deltas';
import { previousName, rangeLabel } from './range';

// Numbers are tabular (MASTER.md); names around them stay proportional.
const numeral = 'm-0 font-normal tracking-numeral leading-none tabular-nums';

const perDayNumber = (n: number) => (Number.isInteger(n) ? `${n}` : n.toFixed(1));

/**
 * Items finished a day over the range against the range before it and the throughput target,
 * the long waits in front of the constraint, and lead time and rework against the range before
 * (Main and Mobile-Floor: Throughput).
 */
export function ThroughputCard({ metrics, className }: { metrics: Metrics; className?: string }) {
  const prev = metrics.previous;
  const vs = previousName(metrics.from, metrics.to);
  const target = metrics.targets.throughput_per_day;
  const perDay = metrics.throughput_per_day;
  const sentences = [
    target !== null ? targetSentence(perDay, target) : null,
    longWaitSentence(metrics.long_waits),
  ].filter(Boolean);
  const leadDelta = leadTimeDelta(metrics.lead_time_hours?.p85, prev?.lead_time_hours?.p85);

  return (
    <SectionCard
      title="Throughput"
      description={
        <span className="max-md:hidden">Items finished per day, {rangeLabel(metrics.from, metrics.to)}</span>
      }
      className={cn('gap-2.5 md:gap-0', className)}
    >
      <p className={cn(numeral, 'text-[52px] md:mt-1 md:mb-2.5 md:text-[64px]')}>
        {perDay.toFixed(1)}
        <span className="ml-1 text-[17px] tracking-normal text-muted md:ml-1.5 md:text-xl"> a day</span>
      </p>
      <p className="m-0 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-sm">
        {prev ? (
          <>
            <DeltaChip delta={throughputDelta(perDay, prev.throughput_per_day)} />
            <span className="text-muted">
              vs {vs}
              <span className="md:hidden">{target !== null ? `, target ${perDayNumber(target)}` : ''}</span>
            </span>
          </>
        ) : null}
      </p>
      {sentences.length > 0 ? (
        <p className="m-0 text-[15px] leading-normal max-md:hidden md:mt-4 md:mb-3.5">{sentences.join(' ')}</p>
      ) : (
        <p className="m-0 text-[15px] leading-normal max-md:hidden md:mt-4 md:mb-3.5">
          {plural(metrics.items_done, 'item')} finished in {plural(metrics.throughput.length, 'day')}.
        </p>
      )}
      {target !== null ? (
        <TargetBar
          value={perDay}
          target={target}
          targetLabel={`Target ${perDayNumber(target)}`}
          label={`${perDay.toFixed(1)} a day against a target of ${perDayNumber(target)}`}
          className="md:mb-3"
        />
      ) : null}
      <div className="flex flex-col">
        <StatRow label="Lead time, p85" className="max-md:pb-0 [&>span:first-child]:text-ink">
          <span className="inline-flex items-center gap-2.5">
            <strong className="font-medium">
              {metrics.lead_time_hours ? formatHours(metrics.lead_time_hours.p85) : 'Not enough data'}
            </strong>
            {leadDelta ? <DeltaChip delta={leadDelta} /> : null}
          </span>
        </StatRow>
        <StatRow label="Rework rate" className="max-md:pb-0 [&>span:first-child]:text-ink">
          <span className="inline-flex items-center gap-2.5">
            <strong className="font-medium">{formatPercent(metrics.rework_rate)}</strong>
            {prev && metrics.items_done > 0 && prev.items_done > 0 ? (
              <DeltaChip delta={reworkDelta(metrics.rework_rate, prev.rework_rate)} />
            ) : null}
          </span>
        </StatRow>
        <StatRow label="Items finished" className="max-md:hidden [&>span:first-child]:text-ink">
          <strong className="font-medium">{metrics.items_done}</strong>
        </StatRow>
      </div>
    </SectionCard>
  );
}

const tones = { worse: 'text-rework', better: 'text-ok-ink', flat: 'text-muted' } as const;

/**
 * A signed change with an arrow, rust when worse and green when better: a white chip from 768
 * px, plain text on phones. The arrow and sign carry the direction; screen readers also hear
 * whether it is better or worse.
 */
function DeltaChip({ delta }: { delta: Delta }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 text-[13px] font-medium whitespace-nowrap tabular-nums md:h-[26px] md:rounded-chip md:bg-surface md:px-2 md:shadow-raise',
        tones[delta.tone],
      )}
    >
      {delta.direction === 'flat' ? null : (
        <svg aria-hidden="true" viewBox="0 0 10 10" className="size-2.5 max-md:hidden">
          <path d={delta.direction === 'up' ? 'M5 1.5 9 8.5H1Z' : 'M5 8.5 9 1.5H1Z'} fill="currentColor" />
        </svg>
      )}
      {delta.text}
      {delta.tone === 'flat' ? null : <span className="sr-only">, {delta.tone}</span>}
    </span>
  );
}
