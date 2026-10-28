// A step under an organization (/o/$org/get-started/<key>). Steps 2 to 8 are built by later parts and
// fill the layout's form and panel; until then the step shows the layout with an empty form.
import { Link } from '@tanstack/react-router';
import type { OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { OnboardingLayout } from './onboarding-layout';
import { type OrgOnboardingStepKey, previousStep } from './steps';

export function OnboardingStepPage({ step, org }: { step: OrgOnboardingStepKey; org: OrgSummary }) {
  const previous = previousStep(step);
  return (
    <OnboardingLayout
      step={step}
      org={org}
      back={
        <Button asChild size="lg" variant="secondary">
          {previous === 'organization' ? (
            <Link to="/get-started">Back</Link>
          ) : (
            <Link to="/o/$org/get-started/$step" params={{ org: org.slug, step: previous }}>
              Back
            </Link>
          )}
        </Button>
      }
    />
  );
}
