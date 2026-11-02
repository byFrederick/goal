import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { ensureLoaded, eventsQuery, itemQuery, itemRunsQuery, partsQuery, touchesQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { ItemSkeleton } from '../app/route-skeletons';
import { NotFoundScreen } from '../app/states';
import { ItemRoute } from '../features/items/item-route';
import { type ItemTab, isItemTab } from '../features/items/item-tabs';
import { itemKey } from '../shared/factory';

// $ref is the item's number in the factory ("12"), a full ref ("goal#12") or its id.
export const Route = createFileRoute('/o/$org/f/$factory/items/$ref')({
  pendingComponent: ItemSkeleton,
  // The selected view. Timeline is the default, so it leaves no `tab` in the URL.
  validateSearch: (search: Record<string, unknown>): { tab?: ItemTab } =>
    isItemTab(search.tab) ? { tab: search.tab } : {},
  loader: async ({ context: { queryClient, org, factory }, params }) => {
    const key = itemKey(factory.name, params.ref);
    // The run records only refine the trace, the line's touches only the touches card's sentence:
    // fetched alongside, never waited for.
    void queryClient.prefetchQuery(itemRunsQuery(org.id, key));
    void queryClient.prefetchQuery(touchesQuery(org.id, factory.id));
    try {
      await Promise.all([
        ensureLoaded(queryClient, itemQuery(org.id, key)),
        ensureLoaded(queryClient, eventsQuery(queryClient, org.id, key)),
        // An item with parts is an assembly, in any state; one that is not has none (total 0).
        ensureLoaded(queryClient, partsQuery(org.id, key)).catch(() => undefined),
      ]);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound();
      throw err;
    }
  },
  head: ({ match: { context }, params }) =>
    pageTitle(itemKey(context.factory.name, params.ref), context.factory.name, context.org.name),
  component: ItemRoute,
  notFoundComponent: () => <NotFoundScreen what="item" />,
});
