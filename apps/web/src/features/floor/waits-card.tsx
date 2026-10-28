// Where work waits on the Floor (Floor, Where work waits): one busy-share bar per agent station the
// range measured, the constraint's bar in amber, and a what-if box for one more agent. The what-if is
// not built yet (no lead-time or cost estimate is shown), so its button is NotAvailable.
import { useQuery } from '@tanstack/react-query';
import { constraintQuery } from '../../api/queries';
import type { ConstraintReport } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { formatDay, formatPercent, humanize } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip, Dot } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { NotAvailable } from '../../ui/not-available';
import { CardEmpty, CardFailed, CardLoading } from './card-states';
import type { FloorRange } from './range';

/** One station's row: its name, its busy bar and its share. */
function BusyRow({ station, ratio, constraint }: { station: string; ratio: number; constraint: boolean }) {
  const pct = Math.round(Math.min(Math.max(ratio, 0), 1) * 100);
  return (
    <div className="flex items-center gap-3 text-body">
      <span className={cn('w-24 shrink-0 truncate text-ink', constraint && 'font-semibold')}>{humanize(station)}</span>
      <span className="block h-2 min-w-0 flex-1 overflow-hidden rounded-chip bg-panel" aria-hidden="true">
        <span
          className={cn('block h-full rounded-chip', constraint ? 'bg-amber' : 'bg-run-soft')}
          style={{ width: `${pct}%` }}
        />
      </span>
      <span className="w-12 shrink-0 text-right text-secondary tabular-nums text-ink">{formatPercent(ratio)}</span>
    </div>
  );
}

export function WaitsCard({ range, className }: { range: FloorRange; className?: string }) {
  const org = useOrg();
  const factory = useFactory();
  const query = useQuery(constraintQuery(org.id, factory.id, range));
  const report = query.data;

  return (
    <SectionCard
      title="Where work waits"
      aria-busy={query.isPending || undefined}
      className={className}
      aside={
        report?.constraint ? (
          <Chip tone="constraint">
            <Dot />
            {humanize(report.constraint)} is the constraint
          </Chip>
        ) : null
      }
    >
      {query.isPending ? <CardLoading /> : null}
      {query.isError ? <CardFailed what="where work waits" onRetry={() => void query.refetch()} /> : null}
      {report ? <Ready report={report} /> : null}
    </SectionCard>
  );
}

function Ready({ report }: { report: ConstraintReport }) {
  const stations = report.candidates;
  if (stations.length === 0) {
    return (
      <CardEmpty>
        {report.coverage_since
          ? `No agent station was busy from ${formatDay(report.from)} to ${formatDay(report.to)}.`
          : 'Not enough history yet: busy shares start with the first agent run.'}
      </CardEmpty>
    );
  }
  const name = report.constraint ? humanize(report.constraint).toLowerCase() : null;
  const article = name && /^[aeiou]/.test(name) ? 'an' : 'a';

  return (
    <>
      <div className="flex flex-col gap-3">
        {stations.map((c) => (
          <BusyRow
            key={c.station}
            station={c.station}
            ratio={c.busy_ratio}
            constraint={c.station === report.constraint}
          />
        ))}
      </div>
      <p className="m-0 text-meta text-muted">Share of the week each station's agents were busy</p>
      {name ? (
        <div className="flex flex-col gap-3 rounded-cell bg-panel p-4 sm:flex-row sm:items-center sm:justify-between">
          <p className="m-0 text-body font-semibold text-ink">With one more {name} agent</p>
          <NotAvailable>
            <Button size="sm" className="self-start sm:self-auto">
              Add {article} {name} agent
            </Button>
          </NotAvailable>
        </div>
      ) : null}
    </>
  );
}
