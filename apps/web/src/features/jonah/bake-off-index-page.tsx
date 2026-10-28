// Jonah's Bake-offs, the tab's own address: it opens the factory's newest bake-off, and shows an empty state
// when there is none. The list is read here, so the page with one bake-off in it is the page of that bake-off.
import { useQuery } from '@tanstack/react-query';
import { Link, Navigate } from '@tanstack/react-router';
import { ApiError } from '../../api/client';
import { bakeOffsQuery } from '../../api/queries';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { EmptyState } from '../../ui/empty-state';
import { BakeOffLoading } from './bake-off-page';
import { JonahTabs } from './jonah-header';

export function BakeOffIndexPage() {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const list = useQuery(bakeOffsQuery(org.id, factory.id));

  if (list.data && list.data.bake_offs.length > 0) {
    const newest = list.data.bake_offs[0];
    if (newest) {
      return (
        <Navigate to="/o/$org/f/$factory/jonah/bake-offs/$bakeOff" params={{ ...params, bakeOff: newest.id }} replace />
      );
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <TopBarSlot crumb="Bake-offs" />
      <JonahTabs tab="bake-offs" counts={list.data ? { 'bake-offs': 0 } : undefined} />
      {list.isPending ? <BakeOffLoading /> : null}
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
            ? 'Evals are not available on this goald yet.'
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
