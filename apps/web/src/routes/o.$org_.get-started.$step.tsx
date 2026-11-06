import { createFileRoute, notFound } from '@tanstack/react-router';
import { ensureFound, orgsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { PendingScreen } from '../app/states';
import { onboardingSearch } from '../features/onboarding/code-search';
import { OnboardingStepPage } from '../features/onboarding/onboarding-step';
import { isOrgOnboardingStep, stepLabel } from '../features/onboarding/steps';
import { findOrg, OrgContext } from '../shared/org';

// Onboarding steps 2 to 8 (ADR 0032 decision 2), under the organization the person just made. The
// trailing underscore keeps this route out of /o/$org's layout: onboarding has no organization
// sidebar.
export const Route = createFileRoute('/o/$org_/get-started/$step')({
  pendingComponent: PendingScreen,
  // The Code step's choice (connection, owner, repos) and the factory step 3 made are in the URL.
  validateSearch: (search: Record<string, unknown>) => onboardingSearch(search),
  beforeLoad: async ({ context, params }) => {
    if (!isOrgOnboardingStep(params.step)) throw notFound();
    const org = await ensureFound(context.queryClient, orgsQuery, (orgs) => findOrg(orgs, params.org));
    if (!org) throw notFound();
    return { org };
  },
  // beforeLoad throws notFound() before it sets `org` for an unknown step, so `org` may be missing here.
  head: ({ match: { context, params } }) => {
    const org: { name: string } | undefined = context.org;
    return pageTitle(
      ...[isOrgOnboardingStep(params.step) ? stepLabel(params.step) : '', org?.name ?? ''].filter(Boolean),
    );
  },
  component: function Step() {
    const { step } = Route.useParams();
    const { org } = Route.useRouteContext();
    const search = Route.useSearch();
    return isOrgOnboardingStep(step) ? (
      <OrgContext value={org}>
        <OnboardingStepPage step={step} org={org} search={search} />
      </OrgContext>
    ) : null;
  },
});
