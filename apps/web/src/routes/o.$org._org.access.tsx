import { createFileRoute } from '@tanstack/react-router';
import { accessQuery, inboxQuery } from '../api/queries';
import type { AccessView } from '../api/types';
import { pageTitle } from '../app/route-focus';
import { ListPageSkeleton } from '../app/route-skeletons';
import { AccessPage } from '../features/access/access-page';
import { type AccessSearch, accessSearch } from '../features/access/search';

// What the organization's agents reach (ADR 0037, canvas "Access"): ?by= (factory or secret; what they
// reach is the default) and ?q= (the search text). The read model loads with the page; the open tool
// approvals fill the Waiting for your OK tile.
export const Route = createFileRoute('/o/$org/_org/access')({
  pendingComponent: ListPageSkeleton,
  validateSearch: (search: Record<string, unknown>): AccessSearch => accessSearch(search),
  loaderDeps: ({ search }) => ({ by: (search.by ?? 'reach') as AccessView }),
  loader: ({ context: { queryClient, org }, deps }) => {
    void queryClient.prefetchQuery(accessQuery(org.id, deps.by));
    void queryClient.prefetchQuery(inboxQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('Access', context.org.name),
  component: AccessPage,
});
