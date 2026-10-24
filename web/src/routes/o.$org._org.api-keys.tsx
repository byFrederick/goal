import { createFileRoute } from '@tanstack/react-router';
import { apiKeysQuery, factoriesQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { ApiKeysPage } from '../features/api-keys/api-keys-page';

// The organization's API keys (spec 3 §10.2 "Api-Keys"). The list loads with the page, not
// before it: when it fails the page keeps its header and New key (the board's "Couldn't load").
export const Route = createFileRoute('/o/$org/_org/api-keys')({
  loader: ({ context: { queryClient, org } }) => {
    void queryClient.prefetchQuery(apiKeysQuery(org.id));
    void queryClient.prefetchQuery(factoriesQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('API keys', context.org.name),
  component: ApiKeysPage,
});
