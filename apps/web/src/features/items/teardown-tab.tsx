// The item's Teardown tab (ADR 0019, decision 3; docs/design/screens/daily-work/teardown.html and
// mobile/teardown.html): where one item's lead time, cost and human help went. goald works it out from the
// item's runs and events on every read; the item stream refetches it, so nothing here polls. Below 1000 px
// the Waiting column goes and Runs is a stacked list.
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { teardownQuery } from '../../api/queries';
import type { ReworkLoop, Teardown, TeardownRun, TeardownStation, TeardownTouch } from '../../api/types';
import { itemKey, useFactory } from '../../shared/factory';
import {
  formatClock,
  formatDurationCompact,
  formatUsdCompact,
  formatUsdCost,
  humanize,
  plural,
} from '../../shared/format';
import { useModelLabel } from '../../shared/models';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { kindLabel } from '../../shared/touches';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { Skeleton } from '../../ui/skeleton';
import { type Stat, StatStrip } from '../../ui/stat-strip';
import { KeepTestDialog } from './keep-test-dialog';
import { touchSentence } from './touch-words';

const span = (seconds: number) => formatDurationCompact(seconds * 1000);

const cardClass = 'min-w-0 overflow-hidden rounded-card border border-line bg-card';
const cardHeader = 'flex min-h-11 flex-wrap items-center gap-x-2 border-b border-line px-4 py-2';
const cardTitle = 'm-0 text-body font-semibold text-ink';
const cardAside = 'ml-auto whitespace-nowrap text-meta tabular-nums text-muted';
const listItem = 'flex min-w-0 flex-col gap-1 border-t border-line px-4 py-3 text-secondary first:border-t-0';
const linkClass =
  'ml-auto inline-flex min-h-11 items-center font-medium text-link no-underline hover:text-link-ink md:min-h-6';

type Tone = 'ok' | 'bad' | 'you' | 'plain';
const dotOf: Record<Tone, string> = { ok: 'bg-ok', bad: 'bg-rework', you: 'bg-you', plain: 'bg-muted' };

/** A status: a small dot in its meaning's colour and the words. Green is finished, red failed, orange a person. */
function Outcome({ tone, children }: { tone: Tone; children: string }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-2 whitespace-nowrap text-meta',
        tone === 'bad' ? 'text-rework-ink' : 'text-ink-soft',
      )}
    >
      <i aria-hidden="true" className={cn('size-2 shrink-0 rounded-full', dotOf[tone])} />
      {children}
    </span>
  );
}

const outcomes: Record<TeardownRun['outcome'], { label: string; tone: Tone }> = {
  running: { label: 'Running', tone: 'plain' },
  finished: { label: 'Finished', tone: 'ok' },
  merged: { label: 'Merged', tone: 'ok' },
  check_failed: { label: 'Check failed', tone: 'bad' },
  sent_back: { label: 'Sent back', tone: 'bad' },
  failed: { label: 'Failed', tone: 'bad' },
  cancelled: { label: 'Cancelled', tone: 'plain' },
};

const loopWords: Record<ReworkLoop['cause'], string> = {
  gate_rejected: 'Sent back',
  rejected: 'Sent back',
  moved: 'Sent back',
  check_failed: 'Check failed',
  run_retried: 'Retried',
};

/** What the loop is about: "Review → Build" for a send-back, "Build ran again" for a failed check or a retry. */
function loopTitle(loop: ReworkLoop): string {
  return loop.to ? `${humanize(loop.station)} → ${humanize(loop.to)}` : `${humanize(loop.station)} ran again`;
}

function statsOf(td: Teardown): Stat[] {
  const lead: Stat = {
    label: 'Lead time',
    value: span(td.lead_time_seconds),
    ...(td.target_seconds
      ? { target: `of a ${span(td.target_seconds)} target`, progress: td.lead_time_seconds / td.target_seconds }
      : { note: td.open ? 'So far' : 'No target set' }),
  };
  const cost: Stat = {
    label: 'Cost',
    value: formatUsdCost(td.cost_usd),
    ...(td.budget_usd
      ? { target: `of a ${formatUsdCompact(td.budget_usd)} budget`, progress: td.cost_usd / td.budget_usd }
      : { note: 'No budget set' }),
  };
  const rework: Stat = {
    label: 'Rework',
    value: plural(td.rework.loops, 'loop'),
    note:
      td.rework.loops === 0
        ? 'Nothing went back'
        : `${span(td.rework.seconds)} and ${formatUsdCost(td.rework.cost_usd)} of the total`,
  };
  const touches: Stat = {
    label: 'Human touches',
    value: td.touch_count,
    note: td.touch_count === 0 ? 'No person had to step in' : `${span(td.touch_wait_seconds)} waiting on a person`,
  };
  return [lead, cost, rework, touches];
}

function lede(td: Teardown, number: number): string {
  const target = td.target_seconds ? ` against a ${span(td.target_seconds)} target` : '';
  return td.open
    ? `Where #${number}’s time, cost and help have gone so far. ${span(td.lead_time_seconds)} in${target}.`
    : `Where #${number}’s time, cost and help went, from intake to merge. Done in ${span(td.lead_time_seconds)}${target}.`;
}

/** A bar that fills from the left, inside a clipping track (MOTION.md, A chart bar or meter fill changes value). */
function Fill({ share, className }: { share: number; className: string }) {
  const [grown, setGrown] = useState(false);
  useEffect(() => {
    const frame = requestAnimationFrame(() => setGrown(true));
    return () => cancelAnimationFrame(frame);
  }, []);
  return (
    <i
      className={cn(
        'block h-full origin-left transition-transform duration-(--duration-fast) ease-(--ease-out) motion-reduce:transition-none',
        className,
      )}
      style={{ width: `${share}%`, transform: grown ? 'scaleX(1)' : 'scaleX(0)' }}
    />
  );
}

function StationBar({ station, total }: { station: TeardownStation; total: number }) {
  const share = (s: number) => (total > 0 ? (s / total) * 100 : 0);
  return (
    <div aria-hidden="true" className="flex h-2 min-w-0 overflow-hidden rounded-chip bg-hover">
      <Fill share={share(station.working_seconds)} className="bg-ink" />
      <Fill share={share(station.waiting_seconds)} className="bg-rule" />
    </div>
  );
}

const th = 'h-9 px-2 text-right text-meta font-medium text-muted first:pl-4 last:pr-4';
const td = 'h-11 whitespace-nowrap border-t border-line px-2 text-right tabular-nums first:pl-4 last:pr-4';
const waiting = 'max-[1000px]:hidden';

function TimeByStation({ td: data }: { td: Teardown }) {
  return (
    <section aria-labelledby="td-time" className={cardClass}>
      <header className={cardHeader}>
        <h2 id="td-time" className={cardTitle}>
          Time by station
        </h2>
        <span className="ml-auto flex gap-3 text-meta text-ink-soft">
          <span className="inline-flex items-center gap-2">
            <i aria-hidden="true" className="size-2 rounded-chip bg-ink" />
            Working
          </span>
          <span className="inline-flex items-center gap-2">
            <i aria-hidden="true" className="size-2 rounded-chip bg-rule" />
            Waiting
          </span>
        </span>
      </header>
      <table className="w-full border-collapse text-secondary">
        <caption className="sr-only">Time by station</caption>
        <thead>
          <tr>
            <th scope="col" className={cn(th, 'w-24 text-left min-[1000px]:w-28')}>
              Station
            </th>
            <th scope="col" className={th}>
              <span className="sr-only">Working against waiting</span>
            </th>
            <th scope="col" className={cn(th, 'w-16 min-[1000px]:w-20')}>
              Working
            </th>
            <th scope="col" className={cn(th, 'w-20', waiting)}>
              Waiting
            </th>
            <th scope="col" className={cn(th, 'w-16 min-[1000px]:w-20')}>
              Total
            </th>
          </tr>
        </thead>
        <tbody>
          {data.stations.map((s) => (
            <tr key={s.station}>
              <th scope="row" className={cn(td, 'text-left font-normal')}>
                <Chip>{humanize(s.station)}</Chip>
              </th>
              <td className={cn(td, 'w-full')}>
                <StationBar station={s} total={data.total.total_seconds} />
              </td>
              <td className={td}>{span(s.working_seconds)}</td>
              <td className={cn(td, waiting)}>{span(s.waiting_seconds)}</td>
              <td className={td}>{span(s.total_seconds)}</td>
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr className="font-semibold">
            <th scope="row" className={cn(td, 'text-left font-semibold')}>
              Total
            </th>
            <td className={td} />
            <td className={td}>{span(data.total.working_seconds)}</td>
            <td className={cn(td, waiting)}>{span(data.total.waiting_seconds)}</td>
            <td className={td}>{span(data.total.total_seconds)}</td>
          </tr>
        </tfoot>
      </table>
    </section>
  );
}

/** The stations this item's runs make candidates for a kept test: one run there, finished, and nothing went back from it. */
export function passedFirstTime(td: Teardown): string[] {
  const wentBack = new Set(td.loops.flatMap((l) => (l.to ? [l.station, l.to] : [l.station])));
  return td.stations
    .map((s) => s.station)
    .filter((station) => {
      const here = td.runs.filter((r) => r.station === station);
      return here.length === 1 && here[0]?.outcome === 'finished' && !wentBack.has(station);
    });
}

function Runs({ td: data, keepable, number }: { td: Teardown; keepable: string[]; number: number }) {
  const model = useModelLabel();
  const factory = useFactory();
  const [keeping, setKeeping] = useState(false);
  const total = data.run_total;
  return (
    <section aria-labelledby="td-runs" className={cardClass}>
      <header className={cardHeader}>
        <h2 id="td-runs" className={cardTitle}>
          Runs
        </h2>
        <span className={cardAside}>
          {plural(total.runs, 'run')}, {plural(total.turns, 'turn')}, {formatUsdCost(total.cost_usd)}
        </span>
        {keepable.length > 0 ? (
          <>
            <Button variant="secondary" size="sm" aria-haspopup="dialog" onClick={() => setKeeping(true)}>
              Keep as a test
            </Button>
            <KeepTestDialog
              open={keeping}
              onOpenChange={setKeeping}
              factoryId={factory.id}
              number={number}
              stations={keepable}
            />
          </>
        ) : null}
      </header>
      <div className="overflow-x-auto max-[1000px]:hidden">
        <table className="w-full border-collapse text-secondary">
          <caption className="sr-only">Runs</caption>
          <thead>
            <tr>
              {['Station', 'Agent', 'Model'].map((h) => (
                <th
                  key={h}
                  scope="col"
                  className="h-9 whitespace-nowrap px-4 text-left text-meta font-medium text-muted"
                >
                  {h}
                </th>
              ))}
              {['Turns', 'Took', 'Cost'].map((h) => (
                <th
                  key={h}
                  scope="col"
                  className="h-9 whitespace-nowrap px-4 text-right text-meta font-medium text-muted"
                >
                  {h}
                </th>
              ))}
              <th scope="col" className="h-9 whitespace-nowrap px-4 text-left text-meta font-medium text-muted">
                Outcome
              </th>
            </tr>
          </thead>
          <tbody>
            {data.runs.map((r) => (
              <tr key={r.id}>
                <td className="h-11 whitespace-nowrap border-t border-line px-4">
                  <Chip>{humanize(r.station)}</Chip>
                </td>
                <td className="h-11 whitespace-nowrap border-t border-line px-4 font-medium">{r.agent}</td>
                <td className="h-11 whitespace-nowrap border-t border-line px-4 text-ink-soft">{model(r.model)}</td>
                <td className="h-11 whitespace-nowrap border-t border-line px-4 text-right tabular-nums">{r.turns}</td>
                <td className="h-11 whitespace-nowrap border-t border-line px-4 text-right tabular-nums">
                  {span(r.seconds)}
                </td>
                <td className="h-11 whitespace-nowrap border-t border-line px-4 text-right tabular-nums">
                  {formatUsdCost(r.cost_usd)}
                </td>
                <td className="h-11 whitespace-nowrap border-t border-line px-4">
                  <RunOutcome run={r} />
                </td>
              </tr>
            ))}
          </tbody>
          <tfoot>
            <tr className="font-semibold">
              <td className="h-11 border-t border-line px-4">Total</td>
              <td className="border-t border-line" />
              <td className="border-t border-line" />
              <td className="whitespace-nowrap border-t border-line px-4 text-right tabular-nums">{total.turns}</td>
              <td className="whitespace-nowrap border-t border-line px-4 text-right tabular-nums">
                {span(total.seconds)}
              </td>
              <td className="whitespace-nowrap border-t border-line px-4 text-right tabular-nums">
                {formatUsdCost(total.cost_usd)}
              </td>
              <td className="border-t border-line" />
            </tr>
          </tfoot>
        </table>
      </div>
      <ol aria-label="Runs" className="m-0 hidden list-none p-0 max-[1000px]:block">
        {data.runs.map((r) => (
          <li key={r.id} className={listItem}>
            <div className="flex min-w-0 items-center gap-2">
              <Chip>{humanize(r.station)}</Chip>
              <span className="min-w-0 truncate font-semibold" title={r.agent}>
                {r.agent}
              </span>
              <span className="ml-auto whitespace-nowrap font-medium tabular-nums">{formatUsdCost(r.cost_usd)}</span>
            </div>
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-meta text-muted">
              <RunOutcome run={r} />
              <span>{model(r.model)}</span>
              <span className="whitespace-nowrap tabular-nums">{plural(r.turns, 'turn')}</span>
              <span className="whitespace-nowrap tabular-nums">{span(r.seconds)}</span>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}

function RunOutcome({ run }: { run: TeardownRun }) {
  const o = outcomes[run.outcome];
  return <Outcome tone={o.tone}>{o.label}</Outcome>;
}

function WhyItWentBack({ loops, number }: { loops: ReworkLoop[]; number: number }) {
  const params = useParams({ from: '/o/$org/f/$factory/items/$ref' });
  const seconds = loops.reduce((n, l) => n + l.seconds, 0);
  const cost = loops.reduce((n, l) => n + l.cost_usd, 0);
  return (
    <section aria-labelledby="td-rw" className={cardClass}>
      <header className={cardHeader}>
        <h2 id="td-rw" className={cardTitle}>
          Why it went back
        </h2>
        <span className={cardAside}>
          {plural(loops.length, 'loop')}, {span(seconds)}, {formatUsdCost(cost)}
        </span>
      </header>
      <ol aria-label={`Why #${number} went back`} className="m-0 list-none p-0">
        {loops.map((l) => (
          <li key={l.event_id} className={listItem}>
            <div className="flex items-center gap-2">
              <Outcome tone="bad">{loopWords[l.cause]}</Outcome>
              <span className="min-w-0 font-medium">{loopTitle(l)}</span>
            </div>
            {l.reason ? <p className="m-0 text-ink-soft [overflow-wrap:anywhere]">{l.reason}</p> : null}
            <div className="flex flex-wrap items-center gap-x-2 text-meta text-muted">
              <span>{formatClock(l.at)}</span>
              <span className="whitespace-nowrap tabular-nums">
                cost {span(l.seconds)}, {formatUsdCost(l.cost_usd)}
              </span>
              <Link
                to="/o/$org/f/$factory/items/$ref"
                params={params}
                search={{ tab: 'trace', step: l.event_id }}
                className={linkClass}
              >
                In the trace
              </Link>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}

function HumanTouches({ touches, count }: { touches: TeardownTouch[]; count: number }) {
  const params = useParams({ from: '/o/$org/f/$factory/items/$ref' });
  return (
    <section aria-labelledby="td-tc" className={cardClass}>
      <header className={cardHeader}>
        <h2 id="td-tc" className={cardTitle}>
          Human touches
        </h2>
        <span className={cardAside}>{count}</span>
      </header>
      <ol aria-label="Human touches" className="m-0 list-none p-0">
        {touches.map(({ touch, wait_seconds }) => (
          <li key={touch.event_id} className={listItem}>
            <div className="flex items-center gap-2">
              <Outcome tone="you">{kindLabel(touch.kind)}</Outcome>
              <span className="min-w-0 font-medium">
                {touch.actor.name} {touchSentence(touch)}
              </span>
            </div>
            <div className="flex flex-wrap items-center gap-x-2 text-meta text-muted">
              <span>
                {touch.station ? `${humanize(touch.station)}, ` : ''}
                {formatClock(touch.at)}
              </span>
              <span className="whitespace-nowrap tabular-nums">waited {span(wait_seconds)}</span>
              <Link to="/o/$org/f/$factory/items/$ref" params={params} search={{}} className={linkClass}>
                In the timeline
              </Link>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}

const block = 'rounded-card bg-hover';

/** The layout as muted blocks, in the slot of the content (MOTION.md, Loading). */
function TeardownPlaceholder() {
  return (
    <div className="flex flex-col gap-4">
      <div className={cn(block, 'h-5 w-2/3')} />
      <div className={cn(block, 'h-24')} />
      <div className={cn(block, 'h-64')} />
      <div className={cn(block, 'h-48')} />
    </div>
  );
}

const footer = 'Worked out from the item’s runs and events each time you open it.';

/** The Teardown of an item: totals, time by station, runs, why it went back and the human touches. */
export function TeardownTab({ number }: { number: number }) {
  const org = useOrg();
  const factory = useFactory();
  const { ref } = useParams({ from: '/o/$org/f/$factory/items/$ref' });
  const q = useQuery(teardownQuery(org.id, itemKey(factory.name, ref)));
  const data = q.data;
  // Keep as a test is for a person who may edit the definition, at a station the item passed the first time.
  const keepable = data && holds(factory, 'definition:edit') ? passedFirstTime(data) : [];

  if (q.isError && !data) {
    return (
      <div role="alert" className="flex flex-col items-start gap-3 rounded-card bg-rework-tint p-4">
        <h2 className="m-0 text-body font-semibold text-rework-ink">Could not work out the breakdown</h2>
        <Callout tone="rework">The item’s runs and events could not be read. Nothing was lost; try again.</Callout>
        <Button variant="secondary" onClick={() => void q.refetch()}>
          Try again
        </Button>
      </div>
    );
  }

  if (data && data.runs.length === 0) {
    return (
      <EmptyState title="No breakdown yet">
        No agent has picked this item up. Time, cost and rework show here once a run starts.
      </EmptyState>
    );
  }

  // With nothing went back and nobody stepped in, Runs takes the full row.
  const hasSide = Boolean(data && (data.loops.length > 0 || data.touches.length > 0));

  return (
    <Skeleton ready={Boolean(data)} placeholder={<TeardownPlaceholder />}>
      {data ? (
        <div className="flex min-w-0 flex-col gap-4 pt-4">
          <p className="m-0 max-w-[72ch] text-secondary text-ink-soft">{lede(data, number)}</p>
          <StatStrip stats={statsOf(data)} label="Totals" />
          <TimeByStation td={data} />
          <div
            data-testid="runs-row"
            className={cn(
              'grid min-w-0 items-start gap-4',
              hasSide && 'min-[1180px]:grid-cols-[minmax(0,1.55fr)_minmax(0,1fr)]',
            )}
          >
            <Runs td={data} keepable={keepable} number={number} />
            {hasSide ? (
              <div className="flex min-w-0 flex-col gap-4">
                {data.loops.length > 0 ? <WhyItWentBack loops={data.loops} number={number} /> : null}
                {data.touches.length > 0 ? <HumanTouches touches={data.touches} count={data.touch_count} /> : null}
              </div>
            ) : null}
          </div>
          <p className="m-0 text-meta text-muted">{footer}</p>
        </div>
      ) : null}
    </Skeleton>
  );
}
