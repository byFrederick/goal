import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { eventsQuery, itemQuery } from '../api/queries';
import { NotFoundScreen } from '../app/states';
import { ItemPage } from '../features/items/item-page';
import { itemKey } from '../shared/factory';

// $ref is the item's number in the factory ("12"), a full ref ("goal#12") or its id.
export const Route = createFileRoute('/f/$factory/items/$ref')({
  loader: async ({ context: { queryClient, factory }, params }) => {
    const key = itemKey(factory.name, params.ref);
    try {
      await Promise.all([
        queryClient.ensureQueryData(itemQuery(key)),
        queryClient.ensureQueryData(eventsQuery(queryClient, key)),
      ]);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound();
      throw err;
    }
  },
  component: ItemPage,
  notFoundComponent: () => <NotFoundScreen what="item" />,
});
