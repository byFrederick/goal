// A step under an organization (/o/$org/get-started/<key>). Step 2 is built (code-step.tsx); the later steps
// are built by later parts and fill the layout's form and panel. Until then a step shows the layout with an
// empty form.
import { Link } from '@tanstack/react-router';
import type { OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import type { CodeSearch } from './code-search';
import { CodeStep } from './code-step';
import { OnboardingLayout } from './onboarding-layout';
import { type OrgOnboardingStepKey, previousStep } from './steps';

export function OnboardingStepPage({
  step,
  org,
  search,
}: {
  step: OrgOnboardingStepKey;
  org: OrgSummary;
  search: CodeSearch;
}) {
  if (step === 'code') return <CodeStep org={org} search={search} />;
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
