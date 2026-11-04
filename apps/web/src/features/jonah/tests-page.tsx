// Jonah's Tests tab (tests.jpg, ADR 0039 decision 12): the past items kept as tests, by station in the line's
// order, each with who kept it and when. A person with definition:edit removes one from its row menu; the item
// stays. The last result of each test comes with the part that runs kept tests at publish, so no column holds it yet.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { MoreHorizontal } from 'lucide-react';
import { toast } from 'sonner';
import { ApiError, removeKeptTest } from '../../api/client';
import { keptTestsQuery, keys } from '../../api/queries';
import type { KeptTest } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { EmptyState } from '../../ui/empty-state';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../ui/menu';
import { Skeleton } from '../../ui/skeleton';
import { useActorName } from './actors';
import { InfoTip } from './info-tip';
import { JonahHeader } from './jonah-header';
import { JonahError } from './jonah-page';

const block = 'rounded-control bg-hover';

/** The tab's layout in muted blocks: a card with its header and three rows. */
function TestsShape() {
  return (
    <div className="flex flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5">
      <div className={`${block} h-5 w-40`} />
      <div className={`${block} h-12`} />
      <div className={`${block} h-12`} />
      <div className={`${block} h-12`} />
    </div>
  );
}

const keptOn = new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric' });

/** What Remove says when it is refused, by the API's code. */
function removeFailure(err: unknown): string {
  return err instanceof ApiError && err.code === 'kept-test-not-found'
    ? 'This test is already gone.'
    : 'goald is not answering. Try again in a moment.';
}

/** The row menu: Remove, for a person with definition:edit. */
function TestMenu({ test }: { test: KeptTest }) {
  const org = useOrg();
  const factory = useFactory();
  const queryClient = useQueryClient();
  const remove = useMutation({
    mutationFn: () => removeKeptTest(org.id, factory.id, test.id),
    onSuccess: async () => {
      toast.success(`Removed #${test.item.number} from the kept tests. The item stays.`);
      await queryClient.invalidateQueries({ queryKey: keptTestsQuery(org.id, factory.id).queryKey });
    },
    onError: async (err) => {
      toast.error(removeFailure(err));
      await queryClient.invalidateQueries({ queryKey: keys.jonah(org.id, factory.id) });
    },
  });
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={`Actions for test #${test.item.number}`}>
          <MoreHorizontal aria-hidden="true" size={16} />
        </Button>
      </MenuTrigger>
      <MenuContent align="end" side="bottom" className="w-44">
        <MenuItem disabled={remove.isPending} onSelect={() => remove.mutate()}>
          Remove
        </MenuItem>
      </MenuContent>
    </Menu>
  );
}

/** The tests of one station: its name and count, then a row per test. */
function StationGroup({ station, tests }: { station: string; tests: KeptTest[] }) {
  const factory = useFactory();
  const actorName = useActorName();
  const canEdit = factory.can.includes('definition:edit');
  return (
    <section aria-label={`${station} tests`}>
      <h3 className="m-0 flex items-baseline gap-2 border-b border-line bg-panel px-4 py-2 text-secondary font-control capitalize text-ink-soft md:px-5">
        {station}
        <span className="text-muted tabular-nums">{tests.length}</span>
      </h3>
      <ul className="m-0 list-none p-0">
        {tests.map((test) => (
          <li
            key={test.id}
            className="grid items-center gap-x-4 gap-y-1 border-b border-line px-4 py-3 last:border-b-0 md:grid-cols-[minmax(0,1fr)_minmax(0,14rem)_2rem] md:px-5"
          >
            <span className="min-w-0 break-words">
              <span className="mr-2 text-muted tabular-nums">#{test.item.number}</span>
              <span className="font-control text-ink">{test.item.title}</span>
            </span>
            <span className="text-secondary text-muted">
              {actorName(test.kept_by)}, {keptOn.format(new Date(test.kept_at))}
            </span>
            {canEdit ? <TestMenu test={test} /> : <span aria-hidden="true" />}
          </li>
        ))}
      </ul>
    </section>
  );
}

/** The kept tests by station: the line's stations in order, then any station the line no longer has. */
export function groupByStation(tests: readonly KeptTest[], order: readonly string[]): [string, KeptTest[]][] {
  const groups = new Map<string, KeptTest[]>();
  for (const station of order) groups.set(station, []);
  for (const test of tests) groups.set(test.station, [...(groups.get(test.station) ?? []), test]);
  return [...groups].filter(([, list]) => list.length > 0);
}

export function TestsPage() {
  const org = useOrg();
  const factory = useFactory();
  const tests = useQuery(keptTestsQuery(org.id, factory.id));
  const groups = tests.data
    ? groupByStation(
        tests.data.tests,
        factory.stations.map((s) => s.id),
      )
    : [];

  return (
    <div className="@container flex min-w-0 flex-col gap-5">
      <TopBarSlot crumb="Tests" />
      <JonahHeader tab="tests" />
      <Skeleton ready={!tests.isPending} placeholder={<TestsShape />}>
        {tests.isError ? (
          <JonahError error={tests.error} retry={() => void tests.refetch()} />
        ) : groups.length === 0 ? (
          <EmptyState title="No kept tests yet">
            Keep a past item that passed first time, and Goal reruns it whenever a change reaches its station.
          </EmptyState>
        ) : (
          <section
            aria-labelledby="kept-h"
            className="flex min-w-0 flex-col overflow-hidden rounded-card border border-line bg-card"
          >
            <div className="flex items-center gap-1 px-4 py-4 md:px-5">
              <h2 id="kept-h" className="m-0 text-heading">
                Kept tests
              </h2>
              <InfoTip label="How kept tests work">
                Goal reruns each kept test when a change reaches its station, and warns before a publish that fails one.
              </InfoTip>
              <span className="ml-2 text-secondary text-muted tabular-nums">{tests.data?.tests.length ?? 0}</span>
            </div>
            {groups.map(([station, list]) => (
              <StationGroup key={station} station={station} tests={list} />
            ))}
          </section>
        )}
      </Skeleton>
    </div>
  );
}
