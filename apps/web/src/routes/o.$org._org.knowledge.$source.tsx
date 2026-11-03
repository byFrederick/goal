import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { ensureLoaded, knowledgeSourceQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { NotFoundScreen, PendingScreen } from '../app/states';
import { type SourceSearch, sourceSearch } from '../features/knowledge/search';
import { SourcePage } from '../features/knowledge/source-page';

// One knowledge source (ADR 0038 decision 12): its files with their reads (?find= filters them), the latest
// sync, settings, the line agents see and who reads it. ?created says it was just added here.
export const Route = createFileRoute('/o/$org/_org/knowledge/$source')({
  pendingComponent: PendingScreen,
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
