// Jonah's Bake-off (ADR 0029, the Jonah screens in ADR 0016): one agent's eval cases run on two to four models,
// what each model passed, what it cost, and which model the report recommends. From here a person runs the
// bake-off again on the current version, or writes the recommended model into the factory's draft. Nothing
// polls: there is no bake-off event on the stream yet, so the page reads again on focus, on its own actions
// and when a definition event refreshes the evals.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { useState } from 'react';
import { ApiError, applyBakeOff, rerunBakeOff } from '../../api/client';
import { bakeOffQuery, bakeOffsQuery, defKeys, keys } from '../../api/queries';
import type { BakeOff } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { EmptyState } from '../../ui/empty-state';
import { RecommendationBanner } from './bake-off-banner';
import { CaseGrid } from './bake-off-cases';
import {
  bakeOffCost,
  bakeOffCrumb,
  bakeOffScope,
  bakeOffTitle,
  currentArm,
  finishedAt,
  recommendationOf,
  statusWords,
} from './bake-off-format';
import { PassCostPlot } from './bake-off-plot';
import { ResultsTable } from './bake-off-results';
import { JonahTabs } from './jonah-header';

/** The breadcrumb's parent crumb: the list of the factory's bake-offs. A constant, so the top bar's effect runs once. */
const bakeOffsCrumb = { label: 'Bake-offs', to: '/o/$org/f/$factory/jonah/bake-offs' } as const;

/** Loading shows the app background and an empty region for the screen reader, never a placeholder shape. */
export function BakeOffLoading() {
  return (
    <div aria-busy="true" className="min-h-40">
      <p className="sr-only">Loading</p>
    </div>
  );
}

/** The subline's second half: the finish when the bake-off is done, otherwise its status chip and what it has cost so far. */
function Status({ bakeOff }: { bakeOff: BakeOff }) {
  if (bakeOff.state === 'done') {
    return <>{`${finishedAt(bakeOff) ?? statusWords.done}, cost ${bakeOffCost(bakeOff)}.`}</>;
  }
  const tone = bakeOff.state === 'running' ? 'run' : bakeOff.state === 'failed' ? 'rework' : 'plain';
  return (
    <>
      <Chip tone={tone} dot>
        {statusWords[bakeOff.state]}
      </Chip>{' '}
      cost {bakeOffCost(bakeOff)} so far.
    </>
  );
}

export function BakeOffPage() {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { bakeOff: bakeOffId } = useParams({ from: '/o/$org/f/$factory/jonah/bake-offs/$bakeOff' });
  const bakeOff = useQuery(bakeOffQuery(org.id, factory.id, bakeOffId));
  const list = useQuery(bakeOffsQuery(org.id, factory.id));
  const [applied, setApplied] = useState(false);

  // The gates: a run and writing the draft both need definition:edit (the API's scope).
  const canEdit = factory.can.includes('definition:edit');

  // The new bake-off is shown at once, from the answer, and the URL moves to it.
  const rerun = useMutation({
    mutationFn: () => rerunBakeOff(org.id, factory.id, bakeOffId),
    onSuccess: ({ bake_off }) => {
      queryClient.setQueryData(bakeOffQuery(org.id, factory.id, bake_off.id).queryKey, bake_off);
      void queryClient.invalidateQueries({ queryKey: keys.bakeOffs(org.id, factory.id) });
      void navigate({
        to: '/o/$org/f/$factory/jonah/bake-offs/$bakeOff',
        params: { ...params, bakeOff: bake_off.id },
      });
    },
  });

  // The model is written into the draft; it never publishes. The draft's queries are read again after it.
  const apply = useMutation({
    mutationFn: (arm: number) => applyBakeOff(org.id, factory.id, bakeOffId, arm),
    onSuccess: () => {
      setApplied(true);
      void queryClient.invalidateQueries({ queryKey: defKeys.draft(org.id, factory.id) });
    },
  });

  if (bakeOff.isPending) {
    return (
      <div className="flex min-w-0 flex-col gap-5">
        <TopBarSlot crumb="Bake-offs" />
        <JonahTabs tab="bake-offs" />
        <BakeOffLoading />
      </div>
    );
  }

  if (bakeOff.isError) {
    const notHere = bakeOff.error instanceof ApiError && bakeOff.error.code === 'eval-run-not-found';
    return (
      <div className="flex min-w-0 flex-col gap-5">
        <TopBarSlot crumb="Bake-offs" />
        <JonahTabs tab="bake-offs" />
        <EmptyState
          title={notHere ? 'This bake-off is not in this factory' : 'The bake-off could not be loaded'}
          actions={
            notHere ? undefined : (
              <Button variant="secondary" onClick={() => void bakeOff.refetch()}>
                Try again
              </Button>
            )
          }
        >
          {notHere
            ? 'It may have been removed. Open the bake-offs of this factory to see the newest.'
            : 'goald is not answering. Try again in a moment.'}
        </EmptyState>
      </div>
    );
  }

  const b = bakeOff.data;
  const recommendation = recommendationOf(b);
  const current = currentArm(b);
  const draftLink = (
    <Button asChild variant="secondary">
      <Link to="/o/$org/f/$factory/definition" params={params}>
        Open the draft
      </Link>
    </Button>
  );
  const applyButton = (
    <Button
      variant="primary"
      disabled={!canEdit || apply.isPending}
      onClick={() => {
        if (b.recommended_arm !== null) apply.mutate(b.recommended_arm);
      }}
    >
      Apply to the definition
    </Button>
  );
  const conflict =
    apply.error instanceof ApiError && apply.error.code === 'draft-conflict' ? (
      <Callout tone="rework">
        The agent changed after this bake-off ran, so its model cannot be written into the draft. Open the{' '}
        <Link to="/o/$org/f/$factory/definition" params={params} className="text-rework-ink underline">
          builder
        </Link>{' '}
        to see the draft.
      </Callout>
    ) : apply.isError ? (
      <Callout tone="rework">The model could not be written into the draft. Try again.</Callout>
    ) : null;

  return (
    <div className="@container flex min-w-0 flex-col gap-5">
      <TopBarSlot
        crumb={bakeOffCrumb(b)}
        parent={bakeOffsCrumb}
        actions={[
          <Button key="run" variant="secondary" disabled={!canEdit || rerun.isPending} onClick={() => rerun.mutate()}>
            Run again
          </Button>,
          <Button key="new" variant="secondary" asChild>
            <Link to="/o/$org/f/$factory/agents" params={params}>
              New bake-off
            </Link>
          </Button>,
        ]}
      />

      <div className="flex flex-col gap-2">
        <h1 className="m-0 text-page-title tracking-display">{bakeOffTitle(b)}</h1>
        <p className="m-0 text-secondary text-ink-soft">
          {bakeOffScope(b)} <Status bakeOff={b} />
        </p>
      </div>

      <JonahTabs tab="bake-offs" counts={list.data ? { 'bake-offs': list.data.bake_offs.length } : undefined} />

      {recommendation && current && b.recommended_arm !== null ? (
        <div aria-live="polite">
          <RecommendationBanner recommendation={recommendation} action={applied ? draftLink : applyButton}>
            {conflict}
          </RecommendationBanner>
        </div>
      ) : null}

      {rerun.isError ? (
        <div aria-live="polite">
          <Callout tone="rework">The bake-off could not be started. Try again.</Callout>
        </div>
      ) : null}

      <div className="grid gap-4 @[960px]:grid-cols-2">
        <SectionCard
          title="Pass rate against cost"
          aside={<span className="text-meta text-muted">Up and left is better</span>}
        >
          <PassCostPlot arms={b.arms} />
        </SectionCard>
        <SectionCard
          title="Tries passed per case"
          aside={<span className="text-meta text-muted">Out of {b.trials}</span>}
        >
          <CaseGrid bakeOff={b} />
        </SectionCard>
      </div>

      <SectionCard title="Results">
        <ResultsTable bakeOff={b} />
      </SectionCard>
    </div>
  );
}
