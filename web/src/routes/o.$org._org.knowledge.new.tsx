import { createFileRoute } from '@tanstack/react-router';
import { connectionKindsQuery, connectionsQuery, ensureLoaded, keys, knowledgeSourcesQuery } from '../api/queries';
import type { Org } from '../api/types';
import { pageTitle } from '../app/route-focus';
import { NewSourcePage } from '../features/knowledge/new-source-page';
import { type NewSourceSearch, newSourceSearch } from '../features/knowledge/search';

// New source (spec 4 §10, canvas "Knowledge-Source-New"): upload, synced or live (?kind=). For
// whoever may add one, the connections and their kinds load before the page (a synced or live
// source picks a connection); the sources load beside it, so a name already taken says so.
export const Route = createFileRoute('/o/$org/_org/knowledge/new')({
  validateSearch: (search: Record<string, unknown>): NewSourceSearch => newSourceSearch(search),
  loader: async ({ context: { queryClient, org } }) => {
    const can = queryClient.getQueryData<Org>(keys.detail(org.id))?.can;
    void queryClient.prefetchQuery(knowledgeSourcesQuery(org.id));
    if (!can?.includes('knowledge:manage') || !can.includes('connection:read')) return;
    await Promise.all([
      ensureLoaded(queryClient, connectionsQuery(org.id)),
      ensureLoaded(queryClient, connectionKindsQuery(org.id)),
    ]);
  },
  head: ({ match: { context } }) => pageTitle('New source', 'Knowledge', context.org.name),
  component: NewSourcePage,
});
