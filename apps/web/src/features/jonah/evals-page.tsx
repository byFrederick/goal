// Jonah's Evals (ADR 0025, the Jonah screens in ADR 0016): the factory's eval cases of the current version,
// how their last tries went, and what a run of them costs. From here a person runs one case, runs them
// all, or adds a case from a past run. Nothing polls. The page reads again when a `definition` event
// (a publish starts an eval run), a resync, or its own action changes what it shows, so an eval run in
// progress shows the state it had when last read: there is no eval event on the stream yet.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Play, Plus } from 'lucide-react';
import { useState } from 'react';
import { ApiError, startEvalRun } from '../../api/client';
import { evalEstimateQuery, evalsQuery, keys } from '../../api/queries';
import type { EvalSummary } from '../../api/types';
import { PendingSkeleton } from '../../app/route-skeletons';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory } from '../../shared/factory';
import { formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { EmptyState } from '../../ui/empty-state';
import { NumberRoll } from '../../ui/number-roll';
import { Skeleton } from '../../ui/skeleton';
import { type Stat, StatStrip } from '../../ui/stat-strip';
import { AddCaseDialog } from './add-case-dialog';
import { firstTryMeta, lastRunMeta, runFailure } from './evals-format';
import { EvalsTable } from './evals-table';
import { JonahHeader } from './jonah-header';

/** The two stats over the summary: the cases that passed every try, and the first-try rate. */
function EvalStats({ summary }: { summary: EvalSummary }) {
  const passingLabel =
    summary.version === undefined ? 'Cases passing every try' : `Cases passing every try, v${summary.version}`;
  const stats: Stat[] = [
    {
      label: passingLabel,
      value: summary.cases_passing,
      unit: `of ${summary.cases}`,
      note: lastRunMeta(summary.latest_run_at, summary.latest_tries_per_case),
    },
    {
      label: 'Pass on first try',
      value: summary.first_try_pass_rate === undefined ? null : Math.round(summary.first_try_pass_rate * 100),
      unit: '%',
      note: firstTryMeta(summary.tries, summary.tries_passed),
    },
  ];
  return <StatStrip label="Last run of all cases" stats={stats} />;
}

const block = 'rounded-control bg-hover';

/** The page's layout in muted blocks: the header and its tabs, the stat strip, the cases card. */
export function EvalsShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <div className={`${block} h-6 w-24`} />
          <div className={`${block} h-4 w-96 max-w-full`} />
        </div>
        <div className={`${block} h-11 w-80 max-w-full md:h-9`} />
      </div>
      <div className="flex gap-6">
        <div className={`${block} h-16 w-48`} />
        <div className={`${block} h-16 w-48`} />
      </div>
      <div className="flex flex-col gap-3 rounded-card border border-line p-4">
        <div className={`${block} h-5 w-40`} />
        <div className={`${block} h-12`} />
        <div className={`${block} h-12`} />
        <div className={`${block} h-12`} />
      </div>
    </div>
  );
}

/** The route's pendingComponent: the page's layout, pulsing until it arrives. */
export function EvalsPending() {
  return <PendingSkeleton placeholder={<EvalsShape />} />;
}

/** Loading shows the page's layout as a skeleton while the data waits. */
function Loading() {
  return (
    <Skeleton ready={false} placeholder={<EvalsShape />}>
      <span />
    </Skeleton>
  );
}

export function EvalsPage() {
  const factory = useFactory();
  const org = useOrg();
  const queryClient = useQueryClient();
  const cases = useQuery(evalsQuery(org.id, factory.id));
  const estimate = useQuery(evalEstimateQuery(org.id, factory.id));
  const [adding, setAdding] = useState(false);

  // The gates: a run is definition:edit, adding a case is definition:publish (the API's scopes).
  const canRun = factory.can.includes('definition:edit');
  const canAdd = factory.can.includes('definition:publish');

  // A run starts at goald and the eval loop works its tries later; the page reads again now, and the
  // stream's `definition` events and a resync do the rest.
  const run = useMutation({
    mutationFn: (name: string | undefined) =>
      startEvalRun(org.id, factory.id, name === undefined ? {} : { case: name }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.evals(org.id, factory.id) }),
  });

  if (cases.isPending) return <Loading />;

  const addButton = (key?: string) => (
    <Button key={key} variant="secondary" disabled={!canAdd} onClick={() => setAdding(true)}>
      <Plus aria-hidden="true" />
      Add a case from a past run
    </Button>
  );

  if (cases.isError) {
    const unavailable = cases.error instanceof ApiError && cases.error.code === 'evals-unavailable';
    return (
      <div className="flex min-w-0 flex-col gap-5">
        <TopBarSlot crumb="Evals" />
        <JonahHeader tab="evals" />
        <EmptyState
          title="Eval cases could not be loaded"
          actions={
            <Button variant="secondary" onClick={() => void cases.refetch()}>
              Try again
            </Button>
          }
        >
          {unavailable
            ? 'Evals are not turned on for this installation yet.'
            : 'goald is not answering. Try again in a moment.'}
        </EmptyState>
      </div>
    );
  }

  const { summary, cases: entries } = cases.data;
  const runAll = estimate.data === undefined ? 'Run all' : `Run all, about ${formatUsd(estimate.data.usd)}`;
  const runAllButton = (
    <Button key="run" variant="primary" disabled={!canRun || run.isPending} onClick={() => run.mutate(undefined)}>
      <Play aria-hidden="true" />
      {runAll}
    </Button>
  );

  return (
    <div className="@container flex min-w-0 flex-col gap-5">
      <TopBarSlot crumb="Evals" actions={entries.length > 0 ? [addButton('add'), runAllButton] : undefined} />
      <JonahHeader tab="evals" counts={{ evals: entries.length }} />

      {entries.length === 0 ? (
        <EmptyState title="No eval cases yet" actions={addButton()}>
          Add a case from a past run, where an agent got something wrong. Every publish then tests it before the new
          version runs.
        </EmptyState>
      ) : (
        <>
          <EvalStats summary={summary} />

          <section
            aria-labelledby="evals-cases-h"
            className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card px-1 pt-4 pb-1"
          >
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4">
              <h2 id="evals-cases-h" className="m-0 text-heading font-semibold">
                Cases
              </h2>
              <Chip>
                <NumberRoll value={entries.length} />
              </Chip>
              <span className="text-secondary text-muted @md:ml-auto">
                In the definition under <span className="font-mono">evals/cases/</span>
              </span>
            </div>
            {run.isError ? (
              <div aria-live="polite" className="px-4">
                <Callout tone="rework">{runFailure(run.error)}</Callout>
              </div>
            ) : null}
            <EvalsTable cases={entries} canRun={canRun} running={run.isPending} onRun={(name) => run.mutate(name)} />
          </section>

          <p className="m-0 text-meta text-muted">Green passed, red failed.</p>
        </>
      )}

      {adding ? <AddCaseDialog org={org} factory={factory} onClose={() => setAdding(false)} /> : null}
    </div>
  );
}
