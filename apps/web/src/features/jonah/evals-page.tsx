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
import { TopBarSlot } from '../../app/top-bar';
import { useFactory } from '../../shared/factory';
import { formatPercent, formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { EmptyState } from '../../ui/empty-state';
import { KpiTile } from '../../ui/kpi-tile';
import { AddCaseDialog } from './add-case-dialog';
import { firstTryMeta, lastRunMeta, none, runFailure } from './evals-format';
import { EvalsTable } from './evals-table';
import { JonahHeader } from './jonah-header';

/** The three tiles over the summary: the cases that passed every try, the first-try rate, and when a publish runs them. */
function EvalTiles({ summary }: { summary: EvalSummary }) {
  const passingLabel =
    summary.version === undefined ? 'Cases passing every try' : `Cases passing every try, v${summary.version}`;
  return (
    <section aria-label="Last run of all cases" className="grid gap-3 @md:grid-cols-3">
      <KpiTile
        label={passingLabel}
        value={`${summary.cases_passing} of ${summary.cases}`}
        target={lastRunMeta(summary.latest_run_at, summary.latest_tries_per_case)}
      />
      <KpiTile
        label="Pass on first try"
        value={summary.first_try_pass_rate === undefined ? none : formatPercent(summary.first_try_pass_rate)}
        target={firstTryMeta(summary.tries, summary.tries_passed)}
      />
      <KpiTile
        label="Runs on"
        value={<span className="text-heading">Every publish</span>}
        target="A failing case blocks the publish"
      />
    </section>
  );
}

/** Loading shows the app background and an empty region for the screen reader, never a placeholder shape. */
function Loading() {
  return (
    <div aria-busy="true" className="min-h-40">
      <p className="sr-only">Loading</p>
    </div>
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
            ? 'Evals are not available on this goald yet.'
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
      <JonahHeader tab="evals" caseCount={entries.length} />

      {entries.length === 0 ? (
        <EmptyState title="No eval cases yet" actions={addButton()}>
          Add a case from a past run, where an agent got something wrong. Every publish then tests it before the new
          version runs.
        </EmptyState>
      ) : (
        <>
          <EvalTiles summary={summary} />

          <SectionCard
            title={
              <span className="inline-flex items-center gap-2">
                Cases
                <Chip>{entries.length}</Chip>
              </span>
            }
            aside={
              <span className="text-secondary text-muted">
                In the definition under <span className="font-mono">evals/cases/</span>
              </span>
            }
          >
            {run.isError ? (
              <div aria-live="polite">
                <Callout tone="rework">{runFailure(run.error)}</Callout>
              </div>
            ) : null}
            <EvalsTable cases={entries} canRun={canRun} running={run.isPending} onRun={(name) => run.mutate(name)} />
          </SectionCard>

          <p className="m-0 text-meta text-muted">
            Green passed, red failed. Add a case from any run where an agent got something wrong, and it is tested
            before every publish.
          </p>
        </>
      )}

      {adding ? <AddCaseDialog org={org} factory={factory} onClose={() => setAdding(false)} /> : null}
    </div>
  );
}
