import { createFileRoute } from '@tanstack/react-router';
import { connectionActivityQuery, connectionKindsQuery, connectionsQuery, factoriesQuery, keys } from '../api/queries';
import type { Org } from '../api/types';
import { pageTitle } from '../app/route-focus';
import { ConnectionsPage } from '../features/connections/connections-page';
import { type ConnectionsSearch, connectionsSearch } from '../features/connections/search';

// The organization's connections (spec 4 §10, canvas "Connections" and "Mobile-Connections"):
// ?factory= (a factory's name: what its current version names; the factory sidebar's
// Connections), ?kind= and ?q= filter the list. The list loads with the page, not before it: when
// it fails the page keeps its header and New connection. The audit entries load only for those
// who may read the audit log (owners and admins).
export const Route = createFileRoute('/o/$org/_org/connections/')({
  validateSearch: (search: Record<string, unknown>): ConnectionsSearch => connectionsSearch(search),
  loaderDeps: ({ search }) => ({ factory: search.factory }),
  loader: ({ context: { queryClient, org }, deps }) => {
    const can = queryClient.getQueryData<Org>(keys.detail(org.id))?.can;
    const factory = deps.factory
      ? queryClient.getQueryData(factoriesQuery(org.id).queryKey)?.find((f) => f.name === deps.factory)
      : undefined;
    if (!deps.factory || factory) {
      void queryClient.prefetchQuery(connectionsQuery(org.id, factory ? { factory: factory.id } : {}));
    }
    void queryClient.prefetchQuery(connectionKindsQuery(org.id));
    if (can?.includes('audit:read')) void queryClient.prefetchQuery(connectionActivityQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('Connections', context.org.name),
  component: ConnectionsPage,
});
