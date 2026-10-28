import { createFileRoute } from '@tanstack/react-router';
import { spendQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { type SpendSearch, spendSearch } from '../features/spend/search';
import { SpendPage } from '../features/spend/spend-page';

// Spend and billing (canvas "Spend and billing", spec 4 §11): ?window=week|month|90d, the window the
// figures cover (week by default). The figures load with the page through getOrgSpend.
export const Route = createFileRoute('/o/$org/_org/spend')({
  validateSearch: (search: Record<string, unknown>): SpendSearch => spendSearch(search),
  loaderDeps: ({ search }) => ({ window: search.window ?? 'week' }),
  // Prefetched, not awaited: a failed answer shows on the page with Try again, not on the route.
  loader: ({ context: { queryClient, org }, deps }) => {
    void queryClient.prefetchQuery(spendQuery(org.id, deps.window));
  },
  head: ({ match: { context } }) => pageTitle('Spend and billing', context.org.name),
  component: SpendPage,
});
