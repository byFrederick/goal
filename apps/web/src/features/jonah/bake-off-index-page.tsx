// Jonah's Bake-offs, the tab's own address: the factory's bake-offs, newest first, each a link to its page,
// and an empty state when there is none (docs/design/screens/jonah/bake-offs.html).
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { bakeOffsQuery } from '../../api/queries';
import { isUnavailable } from '../../api/retry';
import { PendingSkeleton } from '../../app/route-skeletons';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { EmptyState } from '../../ui/empty-state';
import { NotAvailable } from '../../ui/not-available';
import { NumberRoll } from '../../ui/number-roll';
import { bakeOffCost, bakeOffDay, bakeOffScope, bakeOffTitle } from './bake-off-format';
import { JonahHeader } from './jonah-header';

const block = 'rounded-control bg-hover';

/** The list's layout in muted blocks: the header and its tabs, then the rows of a bordered card. */
export function BakeOffsShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <div className={`${block} h-6 w-24`} />
          <div className={`${block} h-4 w-96 max-w-full`} />
        </div>
        <div className={`${block} h-11 w-80 max-w-full md:h-9`} />
      </div>
      <div className="flex flex-col rounded-card border border-line bg-card">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="flex items-center gap-3 border-b border-line px-4 py-3 last:border-b-0">
            <div className={`${block} h-4 w-64 max-w-full`} />
            <div className={`${block} ms-auto h-4 w-20`} />
          </div>
        ))}
      </div>
    </div>
  );
}

/** The route's pendingComponent: the list's layout, pulsing until the page arrives. */
export function BakeOffsPending() {
  return <PendingSkeleton placeholder={<BakeOffsShape />} />;
}

export function BakeOffIndexPage() {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const list = useQuery(bakeOffsQuery(org.id, factory.id));
  // A goald built without evals answers 503 evals-unavailable: no bake-offs yet, and starting one is not available yet.
  const unavailable = list.isError && isUnavailable(list.error);

  if (list.isPending) {
    return (
      <div className="flex min-w-0 flex-col gap-5">
        <TopBarSlot crumb="Bake-offs" />
        <BakeOffsPending />
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <TopBarSlot crumb="Bake-offs" />
      <JonahHeader tab="bake-offs" counts={list.data ? { 'bake-offs': list.data.bake_offs.length } : undefined} />

      {list.data && list.data.bake_offs.length > 0 ? (
        <div className="min-w-0 rounded-card border border-line bg-card">
          <ul aria-label="Bake-offs" className="m-0 flex list-none flex-col p-0">
            {list.data.bake_offs.map((bakeOff) => (
              <li
                key={bakeOff.id}
                className="border-t border-line first:border-t-0 motion-safe:animate-row-in motion-reduce:animate-fade-in"
              >
                <Link
                  to="/o/$org/f/$factory/jonah/bake-offs/$bakeOff"
                  params={{ ...params, bakeOff: bakeOff.id }}
                  className="press flex min-h-11 flex-col gap-1 px-4 py-2 hover:bg-hover md:flex-row md:items-center md:gap-3"
                >
                  <span className="min-w-0 font-control text-ink">{bakeOffTitle(bakeOff)}</span>
                  <span className="text-secondary text-muted md:ms-auto">
                    {bakeOffDay(bakeOff)}. {bakeOffScope(bakeOff)} Cost <NumberRoll value={bakeOffCost(bakeOff)} />.
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {list.isError && !unavailable ? (
        <EmptyState
          title="Bake-offs could not be loaded"
          actions={
            <Button variant="secondary" onClick={() => void list.refetch()}>
              Try again
            </Button>
          }
        >
          goald is not answering. Try again in a moment.
        </EmptyState>
      ) : null}
      {unavailable || (list.data && list.data.bake_offs.length === 0) ? (
        <EmptyState
          title="No bake-offs yet"
          actions={
            unavailable ? (
              <NotAvailable>
                <Button variant="secondary" disabled>
                  New bake-off
                </Button>
              </NotAvailable>
            ) : (
              <Button variant="secondary" asChild>
                <Link to="/o/$org/f/$factory/agents" params={params}>
                  New bake-off
                </Link>
              </Button>
            )
          }
        >
          A bake-off runs one agent's eval cases on several models and shows which model does the work best for the
          money.
        </EmptyState>
      ) : null}
    </div>
  );
}
