// A step under an organization (/o/$org/get-started/<key>). Steps 2 to 5 are built (code-step.tsx,
// factory-step.tsx, line-step.tsx, budget-step.tsx); the later steps are built by later parts and fill the
// layout's form and panel. Until then a step shows the layout with an empty form.
import { Link } from '@tanstack/react-router';
import type { OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { BudgetStep } from './budget-step';
import type { OnboardingSearch } from './code-search';
import { CodeStep } from './code-step';
import { FactoryStep } from './factory-step';
import { LineStep } from './line-step';
import { OnboardingLayout } from './onboarding-layout';
import { type OrgOnboardingStepKey, previousStep } from './steps';

export function OnboardingStepPage({
  step,
  org,
  search,
}: {
  step: OrgOnboardingStepKey;
  org: OrgSummary;
  search: OnboardingSearch;
}) {
  if (step === 'code') return <CodeStep org={org} search={search} />;
  if (step === 'factory') return <FactoryStep org={org} search={search} />;
  if (step === 'line') return <LineStep org={org} search={search} />;
  if (step === 'budget') return <BudgetStep org={org} search={search} />;
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
