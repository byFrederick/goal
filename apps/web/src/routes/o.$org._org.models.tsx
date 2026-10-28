import { createFileRoute } from '@tanstack/react-router';
import { policyQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { ModelsPage } from '../features/models/models-page';

// Models and policies (canvas "Models and policies", ADR 0024): the install's models and the organization's
// defaults over them, read by every member and changed by owners and admins. The policy loads with the page
// through getOrgPolicy; a failed answer shows on the page with Try again, not on the route.
export const Route = createFileRoute('/o/$org/_org/models')({
  loader: ({ context: { queryClient, org } }) => {
    void queryClient.prefetchQuery(policyQuery(org.id));
  },
  head: ({ match: { context } }) => pageTitle('Models and policies', context.org.name),
  component: ModelsPage,
});
