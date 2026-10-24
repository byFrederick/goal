import { createFileRoute } from '@tanstack/react-router';
import { inboxQuery, overviewQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { FactoriesPage, FactoriesPending } from '../features/factories/factories-page';

// The organization's home: its factories' week (GET /overview) and what waits on you (GET /inbox),
// loaded in parallel. The organization comes from the /o/$org layout's context.
export const Route = createFileRoute('/o/$org/_org/')({
  loader: ({ context: { queryClient, org } }) =>
    Promise.all([queryClient.ensureQueryData(overviewQuery(org.id)), queryClient.ensureQueryData(inboxQuery(org.id))]),
  head: ({ match: { context } }) => pageTitle('Factories', context.org.name),
  pendingComponent: FactoriesPending,
  component: FactoriesPage,
});
