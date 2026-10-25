import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { ensureLoaded, eventsQuery, itemQuery, runsQuery, touchesQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { NotFoundScreen } from '../app/states';
import { ItemPage } from '../features/items/item-page';
import { itemKey } from '../shared/factory';

// $ref is the item's number in the factory ("12"), a full ref ("goal#12") or its id.
export const Route = createFileRoute('/o/$org/f/$factory/items/$ref')({
  loader: async ({ context: { queryClient, org, factory }, params }) => {
    const key = itemKey(factory.name, params.ref);
    // The run records only refine the trace, the line's touches only the touches card's sentence:
    // fetched alongside, never waited for.
    void queryClient.prefetchQuery(runsQuery(org.id, key));
    void queryClient.prefetchQuery(touchesQuery(org.id, factory.id));
    try {
      await Promise.all([
        ensureLoaded(queryClient, itemQuery(org.id, key)),
        ensureLoaded(queryClient, eventsQuery(queryClient, org.id, key)),
      ]);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound();
      throw err;
    }
  },
  head: ({ match: { context }, params }) =>
    pageTitle(itemKey(context.factory.name, params.ref), context.factory.name, context.org.name),
  component: ItemPage,
  notFoundComponent: () => <NotFoundScreen what="item" />,
});
