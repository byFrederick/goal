// The Runs page (ADR 0016, wave 2): every agent run in the organization's factories for one day
// (or yesterday), newest first. The KPI tiles and the filter chips read the window's summary; the
// table reads one page of runs under the same filter. Filters, factory, range and search live in
// the URL. Stream events refresh the queries (shared/invalidation.ts); nothing polls.
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { ChevronDown, Download, Search } from 'lucide-react';
import { Fragment, useCallback, useEffect, useId, useMemo, useState } from 'react';
import { exportOrgRuns, type RunsExportFilter } from '../../api/client';
import { exportFailure, saveCsv } from '../../api/download';
import { factoriesQuery, runsQuery } from '../../api/queries';
import type { OrgRun, RunStatus } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { TopBarSlot } from '../../app/top-bar';
import { factoryTitle, refParam } from '../../shared/factory';
import { formatClock, formatDuration, formatUsdCompact, humanize } from '../../shared/format';
import { factoryMark } from '../../shared/marks';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { Input } from '../../ui/field';
import { Menu, MenuContent, MenuRadioGroup, MenuRadioItem, MenuTrigger } from '../../ui/menu';
import { NotAvailable } from '../../ui/not-available';
import { Skeleton } from '../../ui/skeleton';
import { Spinner } from '../../ui/spinner';
import { StatStrip } from '../../ui/stat-strip';
import { type RunsRange, type RunsSearch, rangeWord, runsWindow } from './search';

/** The rows one page asks for: the footer counts what is shown, and paging is not built yet. */
const pageSize = 50;

/** The search waits this long after the last keystroke before the URL, and the query, follow. */
const searchDelayMs = 250;

/**
 * The outcome chips, in the picture's order. "Finished" is the API's succeeded status. "Waiting on you" has no
 * status in the API yet, so it renders disabled through NotAvailable.
 */
const outcomes: {
  label: string;
  status?: RunStatus;
  tone: 'run' | 'rework' | 'ok' | 'you' | 'plain';
  notBuilt?: true;
}[] = [
  { label: 'All', tone: 'plain' },
  { label: 'Running', status: 'running', tone: 'run' },
  { label: 'Waiting on you', tone: 'you', notBuilt: true },
  { label: 'Failed', status: 'failed', tone: 'rework' },
  { label: 'Finished', status: 'succeeded', tone: 'ok' },
];

/** Dot colours for the outcome chips and the spend bar, from the palette tokens only. */

/** Two letters from an agent's name: "backend-engineer" is BE. Agents are named by role, never by copy. */
function initialsOf(agent: string): string {
  return agent
    .split('-')
    .map((part) => part.charAt(0))
    .join('')
    .slice(0, 2)
    .toUpperCase();
}

/** The item's number with a hash in front, as the picture shows it. */
function numberOf(ref: string): string {
  return `#${refParam(ref)}`;
}

/** How long a run took, or how long it has run so far; a run that is still going has no end. */
function durationOf(run: OrgRun, now: number): string {
  const end = run.ended_at ? Date.parse(run.ended_at) : now;
  return formatDuration(end - Date.parse(run.started_at));
}

function Outcome({ status }: { status: RunStatus }) {
  switch (status) {
    case 'running':
      return (
        <Chip tone="run" dot>
          <span className="thinking">Running</span>
        </Chip>
      );
    case 'failed':
      return <Chip tone="rework">Failed</Chip>;
    case 'succeeded':
      return <Chip tone="ok">Finished</Chip>;
    case 'cancelled':
      return <Chip>Stopped</Chip>;
  }
}

export function RunsPage() {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/runs' });
  const navigate = useNavigate({ from: '/o/$org/runs' });
  const factories = useQuery(factoriesQuery(org.id));
  const [text, setText] = useState(search.q ?? '');
  const searchId = useId();

  // The window is the UTC day (or the day before): stable within the day, so the query key is too.
  const day = runsWindow(search.range);
  // The list and its CSV export take the same filter; the export has no page. Memoised so the top bar's
  // actions, which hold the export, are rebuilt only when a filter changes.
  const exportFilter = useMemo<RunsExportFilter>(
    () => ({
      ...(search.factory ? { factory: search.factory } : {}),
      ...(search.status ? { status: search.status } : {}),
      since: day.since,
      until: day.until,
      ...(search.q ? { q: search.q } : {}),
    }),
    [search.factory, search.status, search.q, day.since, day.until],
  );
  const runs = useQuery(runsQuery(org.id, { ...exportFilter, limit: pageSize }));
  const exportRuns = useMutation({
    mutationFn: () => exportOrgRuns(org.id, exportFilter),
    onSuccess: saveCsv,
  });
  const exporting = exportRuns.isPending;

  const setSearch = useCallback(
    (next: Partial<RunsSearch>) =>
      navigate({ search: (prev) => ({ ...prev, ...next }), replace: true, resetScroll: false }),
    [navigate],
  );

  // The search text reaches the URL once the typing pauses; the query follows the URL.
  useEffect(() => {
    const q = text.trim() || undefined;
    if (q === (search.q || undefined)) return;
    const timer = setTimeout(() => setSearch({ q }), searchDelayMs);
    return () => clearTimeout(timer);
  }, [text, search.q, setSearch]);

  const factoryKey = search.factory ?? 'all';
  const rangeKey: RunsRange = search.range ?? 'today';
  const factoryLabel = search.factory
    ? factoryTitle(factories.data?.find((f) => f.name === search.factory) ?? { name: search.factory })
    : 'All factories';

  // The top bar's menus. Memoised: the bar stores what it is given, and a new element each render
  // would set it again each render.
  const actions = useMemo(
    () => (
      <>
        <Menu>
          <MenuTrigger asChild>
            <Button variant="secondary">
              {factoryLabel}
              <ChevronDown size={14} strokeWidth={1.75} aria-hidden="true" />
            </Button>
          </MenuTrigger>
          <MenuContent align="end">
            <MenuRadioGroup
              value={factoryKey}
              onValueChange={(v) => setSearch({ factory: v === 'all' ? undefined : v })}
            >
              <MenuRadioItem value="all">All factories</MenuRadioItem>
              {(factories.data ?? []).map((f) => (
                <MenuRadioItem key={f.id} value={f.name}>
                  {factoryTitle(f)}
                </MenuRadioItem>
              ))}
            </MenuRadioGroup>
          </MenuContent>
        </Menu>
        <Menu>
          <MenuTrigger asChild>
            <Button variant="secondary">
              {rangeKey === 'yesterday' ? 'Yesterday' : 'Today'}
              <ChevronDown size={14} strokeWidth={1.75} aria-hidden="true" />
            </Button>
          </MenuTrigger>
          <MenuContent align="end">
            <MenuRadioGroup
              value={rangeKey}
              onValueChange={(v) => setSearch({ range: v === 'yesterday' ? 'yesterday' : undefined })}
            >
              <MenuRadioItem value="today">Today</MenuRadioItem>
              <MenuRadioItem value="yesterday">Yesterday</MenuRadioItem>
            </MenuRadioGroup>
          </MenuContent>
        </Menu>
        <Button
          variant="secondary"
          aria-busy={exporting || undefined}
          onClick={() => {
            if (!exporting) exportRuns.mutate();
          }}
          className="aria-busy:cursor-progress"
        >
          {exporting ? <Spinner tone="dark" /> : <Download size={14} strokeWidth={1.75} aria-hidden="true" />}
          Export CSV
        </Button>
      </>
    ),
    // exportRuns.mutate is stable; exporting changes the button's busy state.
    [factoryLabel, factoryKey, rangeKey, factories.data, setSearch, exporting, exportRuns.mutate],
  );

  const summary = runs.data?.summary;
  const rows = runs.data?.runs ?? [];
  const now = Date.now();
  const word = rangeWord(search.range);

  return (
    <div className="flex flex-col gap-5">
      <TopBarSlot actions={actions} />
      <div className="flex flex-col gap-1">
        <h1 className="m-0 text-page-title tracking-display">Runs</h1>
        <p className="m-0 text-body text-muted">Every time an agent worked on an item, in all factories.</p>
      </div>

      {exportRuns.isError ? (
        <div role="alert">
          <Callout tone="rework">{exportFailure(exportRuns.error, 'runs')}</Callout>
        </div>
      ) : null}

      {runs.isPending ? (
        <Skeleton ready={false} placeholder={<RunsPlaceholder />}>
          {null}
        </Skeleton>
      ) : runs.isError ? (
        <div
          role="alert"
          className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
        >
          <Callout tone="rework">Couldn’t load runs. Check your connection and try again.</Callout>
          <Button variant="white" onClick={() => void runs.refetch()}>
            Retry
          </Button>
        </div>
      ) : (
        <>
          <StatStrip
            label={word === 'today' ? 'Today' : 'Yesterday'}
            stats={[
              {
                label: 'Runs today',
                value: summary?.total ?? 0,
                note: `${summary?.running ?? 0} running now`,
              },
              { label: 'Failed', value: summary?.failed ?? 0, note: 'Each one starts over' },
              { label: 'Waiting on you', value: null, note: 'Not available yet' },
              {
                label: 'Spend today',
                value: formatUsdCompact(summary?.spend_usd ?? 0),
                note: (summary?.spend_by_factory ?? [])
                  .map((f) => `${f.name} ${formatUsdCompact(f.spend_usd)}`)
                  .join(', '),
              },
            ]}
          />

          <div className="flex flex-wrap items-center gap-2">
            <fieldset className="m-0 flex min-w-0 flex-wrap gap-2 border-0 p-0">
              <legend className="sr-only">Show</legend>
              {outcomes.map((o) => {
                if (o.notBuilt) {
                  return (
                    <NotAvailable key={o.label} label={o.label}>
                      <button
                        type="button"
                        className="inline-flex h-8 items-center gap-2 rounded-control border border-rule bg-card px-3 text-secondary font-control text-ink-soft"
                      >
                        <span className="size-2 rounded-full bg-you" aria-hidden="true" />
                        {o.label}
                      </button>
                    </NotAvailable>
                  );
                }
                const active = o.status === search.status;
                const count =
                  o.label === 'All'
                    ? summary?.total
                    : o.label === 'Running'
                      ? summary?.running
                      : o.label === 'Failed'
                        ? summary?.failed
                        : summary?.finished;
                return (
                  <button
                    key={o.label}
                    type="button"
                    aria-pressed={active}
                    onClick={() => setSearch({ status: o.status })}
                    className={cn(
                      'inline-flex h-8 items-center gap-2 rounded-control border px-3 text-secondary font-control',
                      active ? 'border-ink bg-ink text-shell' : 'border-rule bg-card text-ink-soft hover:bg-panel',
                    )}
                  >
                    {o.tone !== 'plain' ? (
                      <span
                        className={cn(
                          'size-2 rounded-full',
                          o.tone === 'run'
                            ? 'bg-run'
                            : o.tone === 'rework'
                              ? 'bg-rework'
                              : o.tone === 'you'
                                ? 'bg-you'
                                : 'bg-ok',
                        )}
                        aria-hidden="true"
                      />
                    ) : null}
                    {o.label}
                    <span className="text-meta text-muted tabular-nums">{count ?? 0}</span>
                  </button>
                );
              })}
            </fieldset>
            <div className="relative ml-auto w-full min-w-0 sm:w-60">
              <label htmlFor={searchId} className="sr-only">
                Search runs
              </label>
              <Search
                size={16}
                strokeWidth={1.75}
                aria-hidden="true"
                className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
              />
              <Input
                id={searchId}
                type="search"
                value={text}
                placeholder="Item, agent or model"
                autoComplete="off"
                onChange={(e) => setText(e.target.value)}
                className="pl-9"
              />
            </div>
          </div>

          {rows.length === 0 ? (
            <EmptyState
              title={summary?.total ? 'No runs match these filters' : `No runs ${word} yet`}
              actions={
                summary?.total ? (
                  <Button
                    variant="white"
                    onClick={() => {
                      setText('');
                      void navigate({ search: {}, replace: true });
                    }}
                  >
                    Clear filters
                  </Button>
                ) : (
                  <Button variant="white" asChild>
                    <Link to="/o/$org" params={{ org: org.slug }}>
                      Go to Home
                    </Link>
                  </Button>
                )
              }
            >
              {summary?.total
                ? 'Change the outcome, the factory or the search to see other runs.'
                : 'Runs appear here when an agent starts work on an item.'}
            </EmptyState>
          ) : (
            <div className="overflow-x-auto rounded-card border border-line">
              <table className="w-full min-w-max border-collapse text-secondary whitespace-nowrap lg:min-w-0">
                <thead>
                  <tr className="bg-panel text-left text-muted">
                    <th scope="col" className="px-3 py-2 font-medium">
                      Factory
                    </th>
                    <th scope="col" className="px-3 py-2 font-medium">
                      Item
                    </th>
                    <th scope="col" className="px-3 py-2 font-medium">
                      Station
                    </th>
                    <th scope="col" className="px-3 py-2 font-medium">
                      Agent
                    </th>
                    <th scope="col" className="px-3 py-2 font-medium">
                      Model
                    </th>
                    <th scope="col" className="px-3 py-2 font-medium">
                      Started
                    </th>
                    <th scope="col" className="px-3 py-2 text-right font-medium">
                      Duration
                    </th>
                    <th scope="col" className="px-3 py-2 text-right font-medium">
                      Turns
                    </th>
                    <th scope="col" className="px-3 py-2 text-right font-medium">
                      Cost
                    </th>
                    <th scope="col" className="px-3 py-2 font-medium">
                      Outcome
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((run) => (
                    <Fragment key={run.id}>
                      <tr
                        className={cn(
                          'border-t border-line motion-safe:animate-row-in motion-reduce:animate-fade-in',
                          run.status === 'failed' && 'bg-rework-tint/50',
                        )}
                      >
                        <td className="px-3 py-2">
                          <FactoryBadge
                            mark={factoryMark(run.factory, factories.data ?? [run.factory])}
                            className="size-5 text-meta"
                          />
                          <span className="sr-only">{run.factory.title || humanize(run.factory.name)}</span>
                        </td>
                        <td className="px-3 py-2 lg:w-full lg:min-w-40 lg:max-w-0">
                          <Link
                            to="/o/$org/f/$factory/items/$ref"
                            params={{ org: org.slug, factory: run.factory.name, ref: refParam(run.item.ref) }}
                            title={`${numberOf(run.item.ref)} ${run.item.title}`}
                            className="block font-medium text-ink no-underline hover:text-run-ink lg:truncate"
                          >
                            {numberOf(run.item.ref)} {run.item.title}
                          </Link>
                        </td>
                        <td className="px-3 py-2">{humanize(run.station)}</td>
                        <td className="px-3 py-2">
                          <span className="inline-flex items-center gap-2 text-ink-soft">
                            <i
                              className="flex size-5 items-center justify-center rounded-full bg-hover text-meta font-semibold not-italic text-ink-soft"
                              aria-hidden="true"
                            >
                              {initialsOf(run.agent)}
                            </i>
                            {run.agent}
                          </span>
                        </td>
                        <td className="px-3 py-2 text-muted">{run.model}</td>
                        <td className="px-3 py-2 text-muted">{formatClock(run.started_at)}</td>
                        <td className="px-3 py-2 text-right tabular-nums">{durationOf(run, now)}</td>
                        <td className="px-3 py-2 text-right tabular-nums">{run.turns}</td>
                        <td className="px-3 py-2 text-right tabular-nums">{formatUsdCompact(run.cost_usd)}</td>
                        <td className="px-3 py-2">
                          <Outcome status={run.status} />
                        </td>
                      </tr>
                      {run.status === 'failed' ? (
                        <tr className="bg-rework-tint/50">
                          <td />
                          <td colSpan={9} className="px-3 pb-3">
                            <div className="flex flex-wrap items-center gap-3 text-secondary">
                              {run.error ? <span className="text-rework-ink">{run.error}</span> : null}
                              <span className="ml-auto flex gap-3">
                                <NotAvailable label="Open transcript">
                                  <button type="button" className="text-run-ink font-control">
                                    Open transcript
                                  </button>
                                </NotAvailable>
                                <NotAvailable label="Send to Jonah">
                                  <button type="button" className="text-run-ink font-control">
                                    Send to Jonah
                                  </button>
                                </NotAvailable>
                              </span>
                            </div>
                          </td>
                        </tr>
                      ) : null}
                    </Fragment>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {rows.length > 0 ? (
            <p className="m-0 text-meta text-muted">
              Showing {rows.length} of {summary?.total ?? 0} runs {word}.
            </p>
          ) : null}
        </>
      )}
    </div>
  );
}

/** The runs table's skeleton: the stat strip's four figures and the table's first rows, drawn as muted blocks. */
function RunsPlaceholder() {
  return (
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: placeholder rows are fixed in number and never reorder
          <span key={i} className="block h-14 rounded-control bg-hover" />
        ))}
      </div>
      <div className="flex flex-col gap-2">
        {Array.from({ length: 6 }, (_, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: placeholder rows are fixed in number and never reorder
          <span key={i} className="block h-9 rounded-control bg-hover" />
        ))}
      </div>
    </div>
  );
}
