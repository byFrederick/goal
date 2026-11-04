import { createFileRoute } from '@tanstack/react-router';
import { factoriesQuery, secretsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { ListPageSkeleton } from '../app/route-skeletons';
import { type SecretsSearch, secretsSearch } from '../features/secrets/search';
import { SecretsPage } from '../features/secrets/secrets-page';

// The organization's secrets (ADR 0037, canvas "Secrets"): ?filter= (expiring or unused), ?q= (the search
// text) and ?new (the New secret sheet is open). The list loads with the page; the factories name the scopes.
export const Route = createFileRoute('/o/$org/_org/secrets/')({
  pendingComponent: ListPageSkeleton,
  validateSearch: (search: Record<string, unknown>): SecretsSearch => secretsSearch(search),
  loader: ({ context: { queryClient, org } }) => {
    void queryClient.prefetchQuery(secretsQuery(org.id));
    void queryClient.prefetchQuery(factoriesQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('Secrets', context.org.name),
  component: SecretsPage,
});
