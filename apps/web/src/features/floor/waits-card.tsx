// Where work waits on the Floor (Floor, Where work waits): one busy-share bar per agent station the
// range measured, the constraint's bar in amber, and a what-if for one more agent at the constraint.
// The what-if says what one more agent would give (lead time, spend) and adds it through a draft that the
// person reviews and publishes in the Line editor (add-agent.ts).
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { constraintQuery } from '../../api/queries';
import type { ConstraintCandidate, ConstraintReport, Factory } from '../../api/types';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { formatDay, formatPercent, formatUsdCompact, humanize, ordinal } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { holds, reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip, Dot } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { addStationAgent } from './add-agent';
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

export function WaitsCard({
  range,
  leadTimeP85,
  className,
}: {
  range: FloorRange;
  /** The Floor's lead time p85 in hours, or null when no item finished in the range. */
  leadTimeP85: number | null;
  className?: string;
}) {
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
      {report ? <Ready report={report} factory={factory} leadTimeP85={leadTimeP85} /> : null}
    </SectionCard>
  );
}

function Ready({
  report,
  factory,
  leadTimeP85,
}: {
  report: ConstraintReport;
  factory: Factory;
  leadTimeP85: number | null;
}) {
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
  const candidate = stations.find((c) => c.station === report.constraint);

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
      {name && candidate ? (
        <WhatIf candidate={candidate} name={name} factory={factory} leadTimeP85={leadTimeP85} />
      ) : null}
    </>
  );
}

/**
 * One more agent at the constraint: its lead time and spend, and the button that adds it to the draft.
 * Left out when the candidate has no estimate or no spend. The station's slots are min(agents, wip), so
 * an agent at a station whose wip is the cap adds no slot; the button still raises `agents` by one.
 */
function WhatIf({
  candidate,
  name,
  factory,
  leadTimeP85,
}: {
  candidate: ConstraintCandidate;
  name: string;
  factory: Factory;
  leadTimeP85: number | null;
}) {
  const org = useOrg();
  const navigate = useNavigate();
  const params = useFactoryParams();
  const add = useMutation({
    mutationFn: () => addStationAgent(org.id, factory.id, candidate.station),
    onSuccess: () => navigate({ to: '/o/$org/f/$factory/definition/$section', params: { ...params, section: 'line' } }),
  });

  const { estimate, spend_per_slot_day_usd: spend } = candidate;
  if (!estimate || spend === null) return null;

  const canEdit = holds(factory, 'definition:edit');
  const reasonId = `what-if-reason-${candidate.station}`;
  const article = /^[aeiou]/.test(name) ? 'an' : 'a';

  return (
    <div className="flex flex-col gap-3 rounded-cell bg-panel p-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 flex-col gap-1">
        <p className="m-0 text-body font-semibold text-ink">
          With a {ordinal(candidate.slots + 1)} {name} agent
        </p>
        {leadTimeP85 !== null ? (
          <p className="m-0 text-secondary text-ink">
            Lead time {Math.round(leadTimeP85)} h to{' '}
            {Math.round(Math.max(0, leadTimeP85 - estimate.extra_slot_saves_hours))} h
          </p>
        ) : null}
        <p className="m-0 text-secondary text-ink-soft">About {formatUsdCompact(Math.round(spend))} more a day</p>
        {add.isError ? (
          <p role="alert" className="m-0 text-secondary text-rework-ink">
            Could not add the agent. Open the definition to see its draft.{' '}
            <Link
              to="/o/$org/f/$factory/definition"
              params={params}
              className="font-semibold text-run underline hover:text-run-ink"
            >
              Open the definition
            </Link>
          </p>
        ) : null}
      </div>
      <div className="flex flex-col gap-1 sm:items-end">
        <Button
          size="sm"
          className="self-start sm:self-auto"
          aria-disabled={!canEdit || add.isPending || undefined}
          aria-describedby={canEdit ? undefined : reasonId}
          aria-busy={add.isPending || undefined}
          onClick={() => {
            if (canEdit && !add.isPending) add.mutate();
          }}
        >
          Add {article} {name} agent
        </Button>
        {canEdit ? null : (
          <p id={reasonId} className="m-0 text-meta text-muted">
            {reasons.definition(factory.name)}
          </p>
        )}
      </div>
    </div>
  );
}
