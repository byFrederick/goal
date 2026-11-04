import { createFileRoute, notFound } from '@tanstack/react-router';
import { ApiError } from '../api/client';
import { ensureLoaded, factoriesQuery, keys, secretActivityQuery, secretQuery } from '../api/queries';
import type { Org } from '../api/types';
import { pageTitle } from '../app/route-focus';
import { SecretSkeleton } from '../app/route-skeletons';
import { NotFoundScreen } from '../app/states';
import { SecretPage } from '../features/secrets/secret-page';

// One secret (ADR 0037, canvas "Secret"): where it is used, its activity from the audit log (for those who
// may read it), Replace value, Settings and Delete. The secret loads before the page; a name that does not
// exist is the not-found screen.
export const Route = createFileRoute('/o/$org/_org/secrets/$secret')({
  pendingComponent: SecretSkeleton,
  loader: async ({ context: { queryClient, org }, params }) => {
    void queryClient.prefetchQuery(factoriesQuery(org.id));
    try {
      const secret = await ensureLoaded(queryClient, secretQuery(org.id, params.secret));
      const can = queryClient.getQueryData<Org>(keys.detail(org.id))?.can;
      if (can?.includes('audit:read')) void queryClient.prefetchQuery(secretActivityQuery(org.id, secret.id));
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound();
      throw err;
    }
  },
  head: ({ match: { context }, params }) => pageTitle(params.secret, 'Secrets', context.org.name),
  component: SecretPage,
  notFoundComponent: () => <NotFoundScreen what="secret" />,
});
