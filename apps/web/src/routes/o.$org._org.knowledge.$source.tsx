import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { ensureLoaded, knowledgeSourceQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { NotFoundScreen } from '../app/states';
import { type SourceSearch, sourceSearch } from '../features/knowledge/search';
import { SourcePage } from '../features/knowledge/source-page';

// One knowledge source (spec 4 §10): what it reads and its status, Sync now, uploads, its
// documents (?document= opens one's indexed text), its settings, what may search it and Delete.
// ?created says it was just added here.
export const Route = createFileRoute('/o/$org/_org/knowledge/$source')({
  validateSearch: (search: Record<string, unknown>): SourceSearch => sourceSearch(search),
  loader: async ({ context: { queryClient, org }, params }) => {
    try {
      return await ensureLoaded(queryClient, knowledgeSourceQuery(org.id, params.source));
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound();
      throw err;
    }
  },
  head: ({ match: { context }, loaderData, params }) =>
    pageTitle(loaderData?.title ?? params.source, 'Knowledge', context.org.name),
  component: SourcePage,
  notFoundComponent: () => <NotFoundScreen what="knowledge source" />,
});
