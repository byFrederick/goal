import { createFileRoute } from '@tanstack/react-router';
import { apiKeysQuery, factoriesQuery, keys } from '../api/queries';
import type { Org } from '../api/types';
import { pageTitle } from '../app/route-focus';
import { ApiKeysPage } from '../features/api-keys/api-keys-page';

// The organization's API keys (spec 3 §10.2 "Api-Keys"). The list loads with the page, not
// before it: when it fails the page keeps its header and New key (the board's "Couldn't load").
// Members who see only their personal tokens (no api_key:read) never ask for the keys.
export const Route = createFileRoute('/o/$org/_org/api-keys')({
  loader: ({ context: { queryClient, org } }) => {
    const can = queryClient.getQueryData<Org>(keys.detail(org.id))?.can;
    if (can?.includes('api_key:read')) void queryClient.prefetchQuery(apiKeysQuery(org.id));
    void queryClient.prefetchQuery(factoriesQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('API keys', context.org.name),
  component: ApiKeysPage,
});
