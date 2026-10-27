import { useSuspenseQuery } from '@tanstack/react-query';
import { createFileRoute, notFound, useParams } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { ensureLoaded, eventsQuery, itemQuery, partsQuery, runsQuery, touchesQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { NotFoundScreen } from '../app/states';
import { AssemblyPage } from '../features/items/assembly-page';
import { ItemPage } from '../features/items/item-page';
import { type ItemTab, isItemTab } from '../features/items/item-tabs';
import { itemKey, useFactory } from '../shared/factory';
import { useOrg } from '../shared/org';

/** An item in state assembly is its own page (its parts); every other item keeps the item page. */
function ItemRoute() {
  const org = useOrg();
  const factory = useFactory();
  const { ref } = useParams({ from: '/o/$org/f/$factory/items/$ref' });
  const { data: item } = useSuspenseQuery(itemQuery(org.id, itemKey(factory.name, ref)));
  return item.state === 'assembly' ? <AssemblyPage /> : <ItemPage />;
}

// $ref is the item's number in the factory ("12"), a full ref ("goal#12") or its id.
export const Route = createFileRoute('/o/$org/f/$factory/items/$ref')({
  // The selected view. Timeline is the default, so it leaves no `tab` in the URL.
  validateSearch: (search: Record<string, unknown>): { tab?: ItemTab } =>
    isItemTab(search.tab) ? { tab: search.tab } : {},
  loader: async ({ context: { queryClient, org, factory }, params }) => {
    const key = itemKey(factory.name, params.ref);
    // The run records only refine the trace, the line's touches only the touches card's sentence:
    // fetched alongside, never waited for.
    void queryClient.prefetchQuery(runsQuery(org.id, key));
    void queryClient.prefetchQuery(touchesQuery(org.id, factory.id));
    // An assembly's parts, for the page an assembly opens on; an item that is not one has none.
    void queryClient.prefetchQuery(partsQuery(org.id, key));
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
  component: ItemRoute,
  notFoundComponent: () => <NotFoundScreen what="item" />,
});
