import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { connectionActivityQuery, connectionKindsQuery, connectionQuery, ensureLoaded, keys } from '../api/queries';
import type { Org } from '../api/types';
import { pageTitle } from '../app/route-focus';
import { NotFoundScreen } from '../app/states';
import { ConnectionPage } from '../features/connections/connection-page';
import { type ConnectionSearch, connectionSearch } from '../features/connections/search';

// One connection (spec 4 §10, canvas "Connection"): its fields with secrets shown as set, what a
// run gets and its kind's docs, what uses it, its recent use from the audit log (owners and
// admins), Test, Replace secret and Delete. ?created says it was just made here.
export const Route = createFileRoute('/o/$org/_org/connections/$connection')({
  validateSearch: (search: Record<string, unknown>): ConnectionSearch => connectionSearch(search),
  loader: async ({ context: { queryClient, org }, params }) => {
    try {
      const [connection] = await Promise.all([
        ensureLoaded(queryClient, connectionQuery(org.id, params.connection)),
        ensureLoaded(queryClient, connectionKindsQuery(org.id)),
      ]);
      const can = queryClient.getQueryData<Org>(keys.detail(org.id))?.can;
      if (can?.includes('audit:read')) void queryClient.prefetchQuery(connectionActivityQuery(org.id, connection.id));
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound();
      throw err;
    }
  },
  head: ({ match: { context }, params }) => pageTitle(params.connection, 'Connections', context.org.name),
  component: ConnectionPage,
  notFoundComponent: () => <NotFoundScreen what="connection" />,
});
