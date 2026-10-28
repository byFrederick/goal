import { createFileRoute } from '@tanstack/react-router';
import { ensureLoaded, orgsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { OrgStep } from '../features/onboarding/org-step';
import { defaultOrg } from '../shared/org';

// Onboarding step 1 (ADR 0032): a signed-in person with no organization lands here, and anyone
// signed in may create another. Later steps live under /o/$org/get-started/<key>.
export const Route = createFileRoute('/get-started')({
  loader: async ({ context }) => ({ home: defaultOrg(await ensureLoaded(context.queryClient, orgsQuery)) }),
  head: () => pageTitle('Name your organization'),
  component: function OrganizationStep() {
    const { home } = Route.useLoaderData();
    return <OrgStep home={home} />;
  },
});
