// Jonah's Judges (ADR 0025, the Jonah screens in ADR 0016): the factory's judges of the current version,
// each with its last 30 days of production scores. A judge's switch turns it on or off: the page reads the
// judge's file, flips its `enabled` and saves it, which publishes a new version. A refused publish leaves
// the switch as it was and says why. Nothing polls: the page reads again when its own action or a
// `definition` event changes what it shows.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ChartColumn, Check, Plus, Rows3 } from 'lucide-react';
import { useState } from 'react';
import { ApiError, updateJudge } from '../../api/client';
import { judgeQuery, judgesQuery } from '../../api/queries';
import type { JudgeListEntry } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { EmptyState } from '../../ui/empty-state';
import { JonahHeader } from './jonah-header';
import { judgeToggle } from './judge-form';
import { refreshAfterPublish } from './judges-cache';
import { toggleFailure } from './judges-format';
import { NewJudgeDialog } from './judges-new-dialog';
import { JudgesTable } from './judges-table';

/** The three cards over the table: what a judge does, in the order it does it. */
const steps = [
  { icon: Rows3, title: 'Sample', text: 'Each judge reads a share of the runs of the agents it scores.' },
  { icon: ChartColumn, title: 'Score', text: 'It picks an outcome from your rubric and says why in one line.' },
  { icon: Check, title: 'Learn', text: 'Scores under the pass mark go to Jonah, which proposes fixes.' },
] as const;

function HowJudgesWork() {
  return (
    <section className="grid gap-3 sm:grid-cols-3" aria-label="How judges work">
      {steps.map(({ icon: Icon, title, text }) => (
        <div key={title} className="flex items-start gap-3 rounded-card bg-panel p-4 text-secondary">
          <span
            aria-hidden="true"
            className="grid size-7.5 shrink-0 place-items-center rounded-control border border-rule bg-card text-run"
          >
            <Icon size={16} strokeWidth={1.75} />
          </span>
          <span className="flex flex-col gap-0.5">
            <b className="font-control font-semibold">{title}</b>
            {text}
          </span>
        </div>
      ))}
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

export function JudgesPage() {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const queryClient = useQueryClient();
  const judges = useQuery(judgesQuery(org.id, factory.id));
  const [adding, setAdding] = useState(false);

  // Publishing (a switch, a save, a new judge) is definition:publish, the API's scope for it.
  const canPublish = factory.can.includes('definition:publish');

  // A flip reads the judge's whole file first, so the publish keeps everything else in it. The version
  // the caller saw is sent, so a newer one running refuses it (draft-base-moved).
  const toggle = useMutation({
    mutationFn: async (entry: JudgeListEntry) => {
      const current = await queryClient.fetchQuery({ ...judgeQuery(org.id, factory.id, entry.slug), staleTime: 0 });
      return updateJudge(org.id, factory.id, entry.slug, judgeToggle(current, factory.current?.id));
    },
    onSuccess: () => refreshAfterPublish(queryClient, org.id, factory.id),
  });

  const newButton = (
    <Button key="new" variant="primary" disabled={!canPublish} onClick={() => setAdding(true)}>
      <Plus aria-hidden="true" />
      New judge
    </Button>
  );

  if (judges.isPending) return <Loading />;

  if (judges.isError) {
    const unavailable = judges.error instanceof ApiError && judges.error.code === 'judges-unavailable';
    return (
      <div className="flex min-w-0 flex-col gap-5">
        <TopBarSlot crumb="Judges" />
        <JonahHeader tab="judges" />
        <EmptyState
          title="Judges could not be loaded"
          actions={
            <Button variant="secondary" onClick={() => void judges.refetch()}>
              Try again
            </Button>
          }
        >
          {unavailable
            ? 'Judges are not available on this goald yet.'
            : 'goald is not answering. Try again in a moment.'}
        </EmptyState>
      </div>
    );
  }

  const { version, judges: entries, judge_cost_usd: cost } = judges.data;
  const empty = version === null || entries.length === 0;

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <TopBarSlot crumb="Judges" actions={[newButton]} />
      <JonahHeader tab="judges" counts={{ judges: entries.length }} />

      {empty ? (
        <EmptyState title="No judges yet" actions={newButton}>
          Add a judge. It reads a sample of runs and scores them against a rubric you write.
        </EmptyState>
      ) : (
        <>
          <HowJudgesWork />

          <SectionCard
            title={
              <span className="inline-flex items-center gap-2">
                Judges
                <Chip>{entries.length}</Chip>
              </span>
            }
            aside={<span className="text-secondary text-muted">{formatUsd(cost)} in the last 30 days</span>}
          >
            {toggle.isError ? (
              <div aria-live="polite">
                <Callout tone="rework">{toggleFailure(toggle.error)}</Callout>
              </div>
            ) : null}
            <JudgesTable
              judges={entries}
              org={params.org}
              factory={params.factory}
              canPublish={canPublish}
              switching={toggle.isPending ? toggle.variables?.slug : undefined}
              onToggle={(judge) => toggle.mutate(judge)}
            />
          </SectionCard>

          <p className="m-0 text-meta text-muted">
            Judges live in the definition under <span className="font-mono">evals/judges/</span>; editing one here
            publishes a new version.
          </p>
        </>
      )}

      {adding ? <NewJudgeDialog org={org} factory={factory} onClose={() => setAdding(false)} /> : null}
    </div>
  );
}
