import { createFileRoute } from '@tanstack/react-router';
import {
  boardQuery,
  constraintQuery,
  ensureLoaded,
  inboxQuery,
  leadTimeQuery,
  metricsQuery,
  weeklyQuery,
} from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { FloorPage } from '../features/floor/floor-page';
import { type FloorSearch, floorSearch } from '../features/floor/range';

export const Route = createFileRoute('/o/$org/f/$factory/')({
  validateSearch: (search: Record<string, unknown>): FloorSearch => floorSearch(search),
  loaderDeps: ({ search }) => search,
  loader: ({ context: { queryClient, org, factory }, deps }) =>
    Promise.all([
      ensureLoaded(queryClient, boardQuery(org.id, factory.id)),
      ensureLoaded(queryClient, metricsQuery(org.id, factory.id, deps)),
      ensureLoaded(queryClient, weeklyQuery(org.id, factory.id, deps)),
      ensureLoaded(queryClient, leadTimeQuery(org.id, factory.id, deps)),
      ensureLoaded(queryClient, constraintQuery(org.id, factory.id, deps)),
      ensureLoaded(queryClient, inboxQuery(org.id, factory.id)),
    ]),
  head: ({ match: { context } }) => pageTitle('Floor', context.factory.name, context.org.name),
  component: FloorPage,
});
