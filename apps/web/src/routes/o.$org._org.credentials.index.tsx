import { createFileRoute } from '@tanstack/react-router';
import { credentialsQuery, factoriesQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { CredentialsPage } from '../features/credentials/credentials-page';
import { type CredentialsSearch, credentialsSearch } from '../features/credentials/search';

// The organization's credentials (ADR 0017, canvas "Credentials"): ?type= (a type's tab) and ?q=
// (the search text) filter the list. The list loads with the page; the factories name the scopes.
export const Route = createFileRoute('/o/$org/_org/credentials/')({
  validateSearch: (search: Record<string, unknown>): CredentialsSearch => credentialsSearch(search),
  loader: ({ context: { queryClient, org } }) => {
    void queryClient.prefetchQuery(credentialsQuery(org.id));
    void queryClient.prefetchQuery(factoriesQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('Credentials', context.org.name),
  component: CredentialsPage,
});
