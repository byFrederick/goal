import { createFileRoute } from '@tanstack/react-router';
import {
  boardQuery,
  constraintQuery,
  ensureLoaded,
  inboxQuery,
  leadTimeQuery,
  metricsQuery,
  touchesQuery,
  weeklyQuery,
} from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { FloorPage } from '../features/floor/floor-page';
import { type FloorSearch, floorSearch, previousRange, rangeOf } from '../features/floor/range';

export const Route = createFileRoute('/o/$org/f/$factory/')({
  validateSearch: (search: Record<string, unknown>): FloorSearch => floorSearch(search),
  // The touches grouping only arranges the page: the statistics load by the range.
  loaderDeps: ({ search }) => rangeOf(search),
  // The touches panel loads on its own (fetched alongside, never waited for): it says when they fail.
  loader: ({ context: { queryClient, org, factory }, deps }) => {
    void queryClient.prefetchQuery(touchesQuery(org.id, factory.id, deps));
    void queryClient.prefetchQuery(touchesQuery(org.id, factory.id, previousRange(deps)));
    return Promise.all([
      ensureLoaded(queryClient, boardQuery(org.id, factory.id)),
      ensureLoaded(queryClient, metricsQuery(org.id, factory.id, deps)),
      ensureLoaded(queryClient, weeklyQuery(org.id, factory.id, deps)),
      ensureLoaded(queryClient, leadTimeQuery(org.id, factory.id, deps)),
      ensureLoaded(queryClient, constraintQuery(org.id, factory.id, deps)),
      ensureLoaded(queryClient, inboxQuery(org.id, factory.id)),
    ]);
  },
  head: ({ match: { context } }) => pageTitle('Floor', context.factory.name, context.org.name),
  component: FloorPage,
});
