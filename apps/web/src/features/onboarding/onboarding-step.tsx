// A step under an organization (/o/$org/get-started/<key>): steps 2 to 8, each in its own file (code-step.tsx,
// factory-step.tsx, line-step.tsx, budget-step.tsx, intake-step.tsx, people-step.tsx, first-item-step.tsx). Each
// fills the layout's form and panel.
import type { OrgSummary } from '../../api/types';
import { BudgetStep } from './budget-step';
import type { OnboardingSearch } from './code-search';
import { CodeStep } from './code-step';
import { FactoryStep } from './factory-step';
import { FirstItemStep } from './first-item-step';
import { IntakeStep } from './intake-step';
import { LineStep } from './line-step';
import { PeopleStep } from './people-step';
import type { OrgOnboardingStepKey } from './steps';

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
  if (step === 'intake') return <IntakeStep org={org} search={search} />;
  if (step === 'people') return <PeopleStep org={org} search={search} />;
  return <FirstItemStep org={org} search={search} />;
}
