// One factory on the Factories home (C §10.7, canvas "Factories" and "Mobile-Factories"). From
// 768 px: name, health, purpose, its human touches per finished item this week (spec 4 §4.9), the
// 2×2 stats (throughput with a 7-day sparkline and its delta, the constraint, what waits on you,
// the week's spend against the budget) and Open/Definition. On a phone the card is compact (three
// stats, then the touches in a line) and is one link to the factory's floor. A factory
// never published is a draft: its card resumes the New factory wizard instead (D13).
import { Link } from '@tanstack/react-router';
import type { FactoryOverview } from '../../api/types';
import { factoryScreens } from '../../app/screens';
import { formatDay, formatPercent, formatUsd, humanize } from '../../shared/format';
import { perItem } from '../../shared/touches';
import { Button } from '../../ui/button';
import { Stat, StatGrid } from '../../ui/card';
import { Chip, Dot } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { HatchBars, Meter } from '../../ui/hatch';
import {
  constraintNote,
  spendNote,
  type TouchesTrend,
  throughputDelta,
  touchesTrend,
  waitingBreakdown,
} from './sentences';

// Numbers are tabular (MASTER.md); the words around them stay proportional.
const numeral = 'm-0 text-[30px] leading-none font-normal tracking-numeral tabular-nums';
const note = 'm-0 text-[13px] leading-snug';

const definition = factoryScreens.find((s) => s.id === 'definition');

export function FactoryCard({ org, overview, mark }: { org: string; overview: FactoryOverview; mark: string }) {
  const { factory } = overview;
  const params = { org, factory: factory.name };
  const headingId = `factory-${factory.id}`;
  const draft = overview.health === 'draft';
  const resume = { to: '/o/$org/new/$step', params: { org, step: 'start' }, search: { factory: factory.id } } as const;

  return (
    <article
      aria-labelledby={headingId}
      className={cn(
        'relative flex min-w-0 flex-1 flex-col gap-2.5 rounded-card border border-line bg-card p-3.5 transition-colors duration-200 hover:border-muted md:gap-4 md:p-5',
        // On a phone the whole card is the link: its focus ring goes around the card.
        'max-md:has-[h2_a:focus-visible]:outline-2 max-md:has-[h2_a:focus-visible]:outline-offset-2 max-md:has-[h2_a:focus-visible]:outline-run',
      )}
    >
      <div className="flex items-center gap-2.5 md:gap-3">
        <span
          aria-hidden="true"
          className="flex size-9 shrink-0 items-center justify-center rounded-control border border-rule bg-card text-sm font-medium md:size-10 md:text-base"
        >
          {mark}
        </span>
        {/* The version line ("v8, draft of v9 open") arrives with B's Factory.current/draft. */}
        <h2
          id={headingId}
          className="m-0 min-w-0 flex-1 text-xl leading-tight font-normal tracking-[-0.02em] break-words md:text-[26px] md:leading-[1.1] md:tracking-display"
        >
          <Link
            {...(draft ? resume : { to: '/o/$org/f/$factory', params })}
            className="no-underline hover:underline max-md:after:absolute max-md:after:inset-0 max-md:focus-visible:outline-none"
          >
            {factory.name}
          </Link>
        </h2>
        <Health overview={overview} />
      </div>

      {factory.description ? (
        <p className="m-0 text-sm leading-[1.4] text-ink-soft md:min-h-11 md:text-[15px] md:leading-[1.45] md:text-ink">
          {factory.description}
        </p>
      ) : null}

      {draft ? (
        <p className="m-0 text-sm leading-normal text-muted">
          Not published yet. Continue its setup, then publish v1 to start the line.
        </p>
      ) : (
        <>
          <WeekTouches overview={overview} />
          <WeekStats overview={overview} />
          <PhoneStats overview={overview} />
          <PhoneTouches overview={overview} />
        </>
      )}

      <div className="mt-auto hidden flex-wrap gap-2 md:flex">
        <Button asChild variant="primary" size="sm">
          {draft ? (
            <Link {...resume}>Continue setup</Link>
          ) : (
            <Link to="/o/$org/f/$factory" params={params}>
              Open {factory.name}
            </Link>
          )}
        </Button>
        {definition?.available && 'to' in definition ? (
          <Button asChild size="sm">
            <Link to={definition.to} params={params}>
              {definition.label}
            </Link>
          </Button>
        ) : null}
      </div>
    </article>
  );
}

/** Running, Andon pulled (its reason on hover and for screen readers) or Draft; never colour alone. */
function Health({ overview }: { overview: FactoryOverview }) {
  const base = 'inline-flex shrink-0 items-center gap-1.5 text-[13px] font-medium';
  switch (overview.health) {
    case 'running':
      return (
        <span className={cn(base, 'text-ok-ink')}>
          <Dot className="bg-ok" />
          Running
        </span>
      );
    case 'stopped':
      return (
        <span className={cn(base, 'text-rework-ink')} title={overview.andon?.reason}>
          <Dot className="bg-rework" />
          Andon pulled
          {overview.andon ? <span className="sr-only">: {overview.andon.reason}</span> : null}
        </span>
      );
    case 'draft':
      return (
        <span className={cn(base, 'text-muted')}>
          <Dot className="bg-muted" />
          Draft
        </span>
      );
  }
}

const trendTone = { better: 'text-ok-ink', worse: 'text-rework-ink', flat: 'text-muted' } as const;

/** "+0.3 vs last week" in rust when worse, green when better; screen readers hear which. */
function Trend({ trend, long }: { trend: TouchesTrend; long: boolean }) {
  return (
    <span className={cn('whitespace-nowrap tabular-nums', trendTone[trend.tone])}>
      {trend.tone === 'flat' && long ? 'Same as last week' : trend.text}
      {trend.tone === 'flat' || !long ? null : ' vs last week'}
      <span className="sr-only">
        {long ? '' : ' vs last week'}
        {trend.tone === 'flat' ? '' : `, ${trend.tone}`}
      </span>
    </span>
  );
}

/** From 768 px: the week's touches per finished item, touchless share and change from last week. */
function WeekTouches({ overview }: { overview: FactoryOverview }) {
  const { current } = overview.touches;
  const trend = touchesTrend(overview.touches);
  return (
    <div className="hidden rounded-control border border-line bg-card px-3.5 py-3 md:block">
      {current.finished === 0 ? (
        <p className="m-0 text-sm text-muted">No items finished this week, so no human touches to count.</p>
      ) : (
        <>
          <p className="m-0 text-[15px]">
            <span className="text-[22px] leading-none tracking-numeral tabular-nums">
              {perItem(current.per_item_mean)}
            </span>{' '}
            human touches per item
          </p>
          <p className="m-0 mt-0.5 flex flex-wrap gap-x-3 text-sm text-muted">
            <span className="tabular-nums">{formatPercent(current.touchless_rate)} touchless</span>
            {trend ? <Trend trend={trend} long /> : null}
          </p>
        </>
      )}
    </div>
  );
}

/** On a phone: the same in one line under the stats. */
function PhoneTouches({ overview }: { overview: FactoryOverview }) {
  const { current } = overview.touches;
  const trend = touchesTrend(overview.touches);
  if (current.finished === 0) return <p className="m-0 text-sm text-muted md:hidden">No items finished this week</p>;
  return (
    <p className="m-0 text-sm md:hidden">
      <strong className="font-medium tabular-nums">{perItem(current.per_item_mean)}</strong> human touches per item,{' '}
      {formatPercent(current.touchless_rate)} touchless
      {trend ? (
        <>
          {' '}
          <Trend trend={trend} long={false} />
        </>
      ) : null}
    </p>
  );
}

function ConstraintChip({ station }: { station: string }) {
  return (
    <Chip tone="constraint" className="max-w-full">
      <span className="truncate">{humanize(station)}</span>
    </Chip>
  );
}

/** The 2×2 grid from 768 px. */
function WeekStats({ overview }: { overview: FactoryOverview }) {
  const { throughput, constraint, waiting, spend_usd: spend, budget_usd: budget } = overview;
  const delta = throughputDelta(throughput.per_day, throughput.previous_per_day);
  const days = throughput.days;
  const range = days.length > 0 ? `${formatDay(days[0]?.day ?? '')} to ${formatDay(days.at(-1)?.day ?? '')}` : '';
  const spent = spendNote(spend, budget);
  const over = budget !== null && spend > budget;

  return (
    <StatGrid className="hidden md:grid">
      <Stat label="Throughput">
        <dd className="m-0 flex items-end justify-between gap-2">
          <span className="whitespace-nowrap">
            <span className={numeral}>{throughput.per_day.toFixed(1)}</span>
            <span className="ml-1 text-[13px] text-muted">a day</span>
          </span>
          <HatchBars
            bars={days.map((d) => ({ key: d.day, value: d.done }))}
            label={`Items done per day, ${range}: ${days.map((d) => d.done).join(', ')}`}
          />
        </dd>
        <dd
          className={cn(
            note,
            delta.trend === 'up' && 'text-ok-ink',
            delta.trend === 'down' && 'text-rework-ink',
            delta.trend === 'flat' && 'text-muted',
          )}
        >
          {delta.text}
        </dd>
      </Stat>
      <Stat label="Constraint">
        {constraint ? (
          <dd className="m-0 flex">
            <ConstraintChip station={constraint.station} />
          </dd>
        ) : null}
        <dd className={cn(note, 'text-muted')}>{constraintNote(constraint)}</dd>
      </Stat>
      <Stat label="Waiting on you">
        <dd className={cn(numeral, waiting.on_you > 0 ? 'text-you' : 'text-muted')}>{waiting.on_you}</dd>
        <dd className={cn(note, 'text-muted')}>{waitingBreakdown(waiting)}</dd>
      </Stat>
      <Stat label="Spend this week">
        <dd className={numeral}>{formatUsd(spend)}</dd>
        <dd className={cn(note, over ? 'text-rework-ink' : 'text-muted')}>{spent.text}</dd>
        {budget !== null && spent.label ? (
          <dd className="m-0 mt-0.5">
            <Meter
              value={spend}
              max={budget}
              label={spent.label}
              tone={over ? 'rework' : 'run'}
              className="h-1.5 bg-panel"
            />
          </dd>
        ) : null}
      </Stat>
    </StatGrid>
  );
}

/** Three compact stats on a phone. */
function PhoneStats({ overview }: { overview: FactoryOverview }) {
  const { throughput, constraint, waiting } = overview;
  const cell = 'gap-0.5 px-2.5 py-2';
  const value = 'm-0 text-[15px] font-medium whitespace-nowrap';
  return (
    <StatGrid className="grid-cols-3 md:hidden">
      <Stat label="Throughput" className={cell}>
        <dd className={value}>{throughput.per_day.toFixed(1)} a day</dd>
      </Stat>
      <Stat label="Constraint" className={cell}>
        <dd className="m-0 flex">
          {constraint ? <ConstraintChip station={constraint.station} /> : <span className={value}>None</span>}
        </dd>
      </Stat>
      <Stat label="Waiting" className={cell}>
        <dd className={cn(value, waiting.on_you > 0 ? 'text-you' : 'text-muted')}>{waiting.on_you}</dd>
      </Stat>
    </StatGrid>
  );
}
