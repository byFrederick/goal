// Jonah's Bake-offs, the tab's own address: the factory's bake-offs, newest first, each a link to its page,
// and an empty state when there is none (docs/design/screens/jonah/bake-offs.html).
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ApiError } from '../../api/client';
import { bakeOffsQuery } from '../../api/queries';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { EmptyState } from '../../ui/empty-state';
import { bakeOffCost, bakeOffDay, bakeOffScope, bakeOffTitle } from './bake-off-format';
import { BakeOffLoading } from './bake-off-page';
import { JonahHeader } from './jonah-header';

export function BakeOffIndexPage() {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const list = useQuery(bakeOffsQuery(org.id, factory.id));

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <TopBarSlot crumb="Bake-offs" />
      <JonahHeader tab="bake-offs" counts={list.data ? { 'bake-offs': list.data.bake_offs.length } : undefined} />
      {list.isPending ? <BakeOffLoading /> : null}
      {list.data && list.data.bake_offs.length > 0 ? (
        <ul aria-label="Bake-offs" className="m-0 flex list-none flex-col p-0">
          {list.data.bake_offs.map((bakeOff) => (
            <li key={bakeOff.id} className="border-b border-line">
              <Link
                to="/o/$org/f/$factory/jonah/bake-offs/$bakeOff"
                params={{ ...params, bakeOff: bakeOff.id }}
                className="flex flex-col gap-1 py-3 hover:bg-hover"
              >
                <span className="font-control font-semibold text-ink">{bakeOffTitle(bakeOff)}</span>
                <span className="text-secondary text-muted">
                  {bakeOffDay(bakeOff)}. {bakeOffScope(bakeOff)} Cost {bakeOffCost(bakeOff)}.
                </span>
              </Link>
            </li>
          ))}
        </ul>
      ) : null}
      {list.isError ? (
        <EmptyState
          title="Bake-offs could not be loaded"
          actions={
            <Button variant="secondary" onClick={() => void list.refetch()}>
              Try again
            </Button>
          }
        >
          {list.error instanceof ApiError && list.error.code === 'evals-unavailable'
            ? 'Evals are not turned on for this installation yet.'
            : 'goald is not answering. Try again in a moment.'}
        </EmptyState>
      ) : null}
      {list.data && list.data.bake_offs.length === 0 ? (
        <EmptyState
          title="No bake-offs yet"
          actions={
            <Button variant="secondary" asChild>
              <Link to="/o/$org/f/$factory/agents" params={params}>
                New bake-off
              </Link>
            </Button>
          }
        >
          A bake-off runs one agent's eval cases on several models and shows which model does the work best for the
          money.
        </EmptyState>
      ) : null}
    </div>
  );
}
