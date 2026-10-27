import { createFileRoute } from '@tanstack/react-router';
import {
  activityQuery,
  boardQuery,
  constraintQuery,
  ensureLoaded,
  inboxQuery,
  metricsQuery,
  weeklyQuery,
} from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { FloorPage } from '../features/floor/floor-page';
import { type FloorSearch, floorSearch, rangeOf } from '../features/floor/range';

export const Route = createFileRoute('/o/$org/f/$factory/')({
  validateSearch: (search: Record<string, unknown>): FloorSearch => floorSearch(search),
  // The range is the one the figures and the cards load by.
  loaderDeps: ({ search }) => rangeOf(search),
  // The cards that hold the page's figures load before it shows; the others are fetched alongside, and
  // each card says when its own data is loading or has failed.
  loader: ({ context: { queryClient, org, factory }, deps }) => {
    void queryClient.prefetchQuery(weeklyQuery(org.id, factory.id, deps));
    void queryClient.prefetchQuery(inboxQuery(org.id, factory.id));
    void queryClient.prefetchQuery(activityQuery(org.id, factory.id));
    return Promise.all([
      ensureLoaded(queryClient, boardQuery(org.id, factory.id)),
      ensureLoaded(queryClient, metricsQuery(org.id, factory.id, deps)),
      ensureLoaded(queryClient, constraintQuery(org.id, factory.id, deps)),
    ]);
  },
  head: ({ match: { context } }) => pageTitle('Floor', context.factory.name, context.org.name),
  component: FloorPage,
});
