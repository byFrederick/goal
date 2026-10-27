// One factory on Home (ADR 0016, wave 2, the picture's "Factories" list): its letter badge, its name
// and health, its navy line (MiniLine), then three figures: what needs you, the throughput or, while
// the andon is pulled, the worst send-back share, and what it spent today. A draft has no line yet.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { boardQuery, metricsQuery } from '../../api/queries';
import type { FactoryOverview, OrgSummary } from '../../api/types';
import { formatPercent, formatUsd } from '../../shared/format';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { MiniLine } from './mini-line';

export function FactoryRow({
  org,
  overview,
  today,
  mark,
}: {
  org: OrgSummary;
  overview: FactoryOverview;
  /** This factory's overview for today (window 1d); its spend shows "…" until it loads. */
  today: FactoryOverview | undefined;
  mark: string;
}) {
  const { factory, health, andon, constraint } = overview;
  const headingId = `factory-${factory.id}`;
  const draft = health === 'draft';
  const stopped = health === 'stopped';
  const resume = {
    to: '/o/$org/new/$step',
    params: { org: org.slug, step: 'start' },
    search: { factory: factory.id },
  } as const;

  return (
    <article
      aria-labelledby={headingId}
      className={cn(
        'flex min-w-0 flex-col gap-4 rounded-card border bg-card p-4 md:flex-row md:items-center md:gap-5 md:p-5',
        stopped ? 'border-rework/40' : 'border-line',
      )}
    >
      <div className="flex min-w-0 items-center gap-3 md:w-44 md:shrink-0">
        <span
          aria-hidden="true"
          className="flex size-9 shrink-0 items-center justify-center rounded-control bg-run font-numeral text-body font-semibold text-white"
        >
          {mark}
        </span>
        <div className="flex min-w-0 flex-col items-start gap-1">
          <h2 id={headingId} className="m-0 min-w-0 text-heading break-words">
            <Link
              {...(draft ? resume : { to: '/o/$org/f/$factory', params: { org: org.slug, factory: factory.name } })}
              className="no-underline hover:underline"
            >
              {factory.name}
            </Link>
          </h2>
          {draft ? (
            <Chip>Draft</Chip>
          ) : stopped ? (
            <Chip tone="rework">
              Andon pulled
              {andon ? <span className="sr-only">: {andon.reason}</span> : null}
            </Chip>
          ) : (
            <Chip tone="ok" dot>
              Running
            </Chip>
          )}
        </div>
      </div>

      {draft ? (
        <p className="m-0 text-secondary text-muted">
          Not published yet. Continue its setup, then publish v1 to start the line.
        </p>
      ) : (
        <>
          <MiniLine
            org={org.id}
            factory={factory.id}
            name={factory.name}
            constraint={constraint?.station ?? null}
            className="min-w-0 flex-1"
          />
          <Figures org={org.id} overview={overview} today={today} />
        </>
      )}
    </article>
  );
}

/** The three figures. Throughput gives way to the worst send-back share while the andon is pulled. */
function Figures({
  org,
  overview,
  today,
}: {
  org: string;
  overview: FactoryOverview;
  today: FactoryOverview | undefined;
}) {
  const { factory, waiting, andon } = overview;
  const target = useQuery(metricsQuery(org, factory.id)).data?.targets.throughput_per_day ?? null;
  const board = useQuery(boardQuery(org, factory.id)).data;
  const worst = andon && board ? highestSendBack(board.stations) : null;
  const spent = today?.spend_usd ?? null;
  const budget = today?.budget_usd ?? null;

  return (
    <dl className="m-0 grid shrink-0 grid-cols-3 gap-4 md:flex md:gap-6">
      <Figure label="Needs you">
        <span className={cn('font-numeral text-kpi font-semibold', waiting.on_you > 0 ? 'text-you-ink' : 'text-muted')}>
          {waiting.on_you}
        </span>
      </Figure>
      {worst !== null ? (
        <Figure label="Sent back">
          <span className="font-numeral text-kpi font-semibold text-rework-ink">{formatPercent(worst)}</span>
        </Figure>
      ) : (
        <Figure label="Throughput">
          <span className="font-numeral text-kpi font-semibold">{overview.throughput.per_day.toFixed(1)}</span>
          <span className="text-meta text-muted">{target !== null ? ` of ${target} a day` : ' a day'}</span>
        </Figure>
      )}
      <Figure label="Spend today">
        <span className="font-numeral text-kpi font-semibold">{spent === null ? '…' : formatUsd(spent)}</span>
        {spent !== null && budget !== null ? (
          <span className="text-meta text-muted"> of {formatUsd(budget)}</span>
        ) : null}
      </Figure>
    </dl>
  );
}

function Figure({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-meta text-muted">{label}</dt>
      <dd className="m-0 whitespace-nowrap tabular-nums">{children}</dd>
    </div>
  );
}

/** The station with the highest send-back share, when any station has one. */
function highestSendBack(stations: { send_back_ratio: number | null }[]): number | null {
  const ratios = stations.flatMap((s) => (s.send_back_ratio === null ? [] : [s.send_back_ratio]));
  return ratios.length > 0 ? Math.max(...ratios) : null;
}
