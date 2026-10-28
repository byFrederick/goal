import { createFileRoute, notFound } from '@tanstack/react-router';
import { ensureFound, orgsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { OnboardingStepPage } from '../features/onboarding/onboarding-step';
import { isOrgOnboardingStep, stepLabel } from '../features/onboarding/steps';
import { findOrg } from '../shared/org';

// Onboarding steps 2 to 8 (ADR 0032 decision 2), under the organization the person just made. The
// trailing underscore keeps this route out of /o/$org's layout: onboarding has no organization
// sidebar.
export const Route = createFileRoute('/o/$org_/get-started/$step')({
  beforeLoad: async ({ context, params }) => {
    if (!isOrgOnboardingStep(params.step)) throw notFound();
    const org = await ensureFound(context.queryClient, orgsQuery, (orgs) => findOrg(orgs, params.org));
    if (!org) throw notFound();
    return { org };
  },
  head: ({ match: { context, params } }) =>
    pageTitle(isOrgOnboardingStep(params.step) ? stepLabel(params.step) : '', context.org.name),
  component: function Step() {
    const { step } = Route.useParams();
    const { org } = Route.useRouteContext();
    return isOrgOnboardingStep(step) ? <OnboardingStepPage step={step} org={org} /> : null;
  },
});
