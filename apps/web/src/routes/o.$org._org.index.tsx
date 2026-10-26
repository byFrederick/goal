import { createFileRoute } from '@tanstack/react-router';
import { archivedFactoriesQuery, ensureLoaded, inboxQuery, overviewQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { FactoriesPage } from '../features/factories/factories-page';
import { type FactoriesSearch, factoriesSearch } from '../features/factories/search';

// The organization's home: its factories' week (GET /overview) and what waits on you (GET /inbox),
// loaded in parallel. ?archived=true is the Archived filter: its list loads with the page too. The
// organization comes from the /o/$org layout's context.
export const Route = createFileRoute('/o/$org/_org/')({
  validateSearch: (search: Record<string, unknown>): FactoriesSearch => factoriesSearch(search),
  loaderDeps: ({ search }) => ({ archived: search.archived }),
  loader: ({ context: { queryClient, org }, deps }) =>
    Promise.all([
      ensureLoaded(queryClient, overviewQuery(org.id)),
      ensureLoaded(queryClient, inboxQuery(org.id)),
      ...(deps.archived ? [ensureLoaded(queryClient, archivedFactoriesQuery(org.id))] : []),
    ]),
  head: ({ match: { context } }) => pageTitle('Factories', context.org.name),
  component: FactoriesRoute,
});

function FactoriesRoute() {
  const { archived } = Route.useSearch();
  return <FactoriesPage archived={archived === true} />;
}
