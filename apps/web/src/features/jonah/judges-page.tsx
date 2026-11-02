// Jonah's Judges (ADR 0025, the Jonah screens in ADR 0016): the factory's judges of the current version,
// each with its last 30 days of production scores. A judge's switch turns it on or off: the page reads the
// judge's file, flips its `enabled` and saves it, which publishes a new version. A refused publish leaves
// the switch as it was and says why. Nothing polls: the page reads again when its own action or a
// `definition` event changes what it shows.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { useState } from 'react';
import { updateJudge } from '../../api/client';
import { judgeQuery, judgesQuery } from '../../api/queries';
import { isUnavailable } from '../../api/retry';
import type { JudgeListEntry } from '../../api/types';
import { PendingSkeleton } from '../../app/route-skeletons';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { EmptyState } from '../../ui/empty-state';
import { NotAvailable } from '../../ui/not-available';
import { NumberRoll } from '../../ui/number-roll';
import { Skeleton } from '../../ui/skeleton';
import { JonahHeader } from './jonah-header';
import { judgeToggle } from './judge-form';
import { refreshAfterPublish } from './judges-cache';
import { toggleFailure } from './judges-format';
import { NewJudgeDialog } from './judges-new-dialog';
import { JudgesTable } from './judges-table';

const block = 'rounded-control bg-hover';

/** The judges page's layout in muted blocks: the header and its tabs, then the card with its table rows. */
export function JudgesShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <div className={`${block} h-6 w-24`} />
          <div className={`${block} h-4 w-96 max-w-full`} />
        </div>
        <div className={`${block} h-11 w-80 max-w-full md:h-9`} />
      </div>
      <div className="flex flex-col gap-3 rounded-card border border-line p-4">
        <div className="flex items-center justify-between">
          <div className={`${block} h-5 w-28`} />
          <div className={`${block} h-4 w-40`} />
        </div>
        {[0, 1, 2, 3].map((n) => (
          <div key={n} className={`${block} h-10`} />
        ))}
      </div>
    </div>
  );
}

/** The route's pendingComponent: the page's layout, pulsing until it arrives. */
export function JudgesPending() {
  return <PendingSkeleton placeholder={<JudgesShape />} />;
}

/** Loading shows the page's layout as a skeleton while the judges wait. */
function Loading() {
  return (
    <Skeleton ready={false} placeholder={<JudgesShape />}>
      <span />
    </Skeleton>
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

  // A goald built without judges answers 503 judges-unavailable: the page is the empty one, creation not available yet.
  const unavailable = judges.isError && isUnavailable(judges.error);
  const newButton = unavailable ? (
    <NotAvailable>
      <Button key="new" variant="primary" disabled>
        <Plus aria-hidden="true" />
        New judge
      </Button>
    </NotAvailable>
  ) : (
    <Button key="new" variant="primary" disabled={!canPublish} onClick={() => setAdding(true)}>
      <Plus aria-hidden="true" />
      New judge
    </Button>
  );

  if (judges.isPending) return <Loading />;

  if (judges.isError && !unavailable) {
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
          goald is not answering. Try again in a moment.
        </EmptyState>
      </div>
    );
  }

  const {
    version,
    judges: entries,
    judge_cost_usd: cost,
  } = judges.data ?? { version: null, judges: [], judge_cost_usd: 0 };
  const empty = version === null || entries.length === 0;

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <TopBarSlot crumb="Judges" actions={empty ? undefined : [newButton]} />
      <JonahHeader tab="judges" counts={{ judges: entries.length }} />

      {empty ? (
        <EmptyState title="No judges yet" actions={newButton}>
          Add a judge. It reads a sample of runs and scores them against a rubric you write.
        </EmptyState>
      ) : (
        <>
          <SectionCard
            title={
              <span className="inline-flex items-center gap-2">
                Judges
                <Chip>
                  <NumberRoll value={entries.length} />
                </Chip>
              </span>
            }
            aside={
              <span className="text-secondary text-muted">
                <NumberRoll value={formatUsd(cost)} /> in the last 30 days
              </span>
            }
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
