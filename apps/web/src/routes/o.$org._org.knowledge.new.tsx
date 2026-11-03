import { createFileRoute } from '@tanstack/react-router';
import { ensureLoaded, keys, knowledgeSourcesQuery, secretsQuery } from '../api/queries';
import type { Org } from '../api/types';
import { pageTitle } from '../app/route-focus';
import { PendingScreen } from '../app/states';
import { NewSourcePage } from '../features/knowledge/new-source-page';
import { type NewSourceSearch, newSourceSearch } from '../features/knowledge/search';

// Add a source (ADR 0038 decision 12, canvas "Knowledge-New"): upload, repository or confluence (?kind=). For
// whoever may add one, the secrets load before the page (a repository or Confluence source picks one); the sources
// load beside it, so a name already taken says so.
export const Route = createFileRoute('/o/$org/_org/knowledge/new')({
  pendingComponent: PendingScreen,
  validateSearch: (search: Record<string, unknown>): NewSourceSearch => newSourceSearch(search),
  loader: async ({ context: { queryClient, org } }) => {
    const can = queryClient.getQueryData<Org>(keys.detail(org.id))?.can;
    void queryClient.prefetchQuery(knowledgeSourcesQuery(org.id));
    if (!can?.includes('knowledge:manage')) return;
    await ensureLoaded(queryClient, secretsQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('New source', 'Knowledge', context.org.name),
  component: NewSourcePage,
});
