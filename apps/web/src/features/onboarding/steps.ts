// The onboarding steps (ADR 0032 decision 2): their order, their labels and their keys. Step 1 is
// /get-started; every later step is /o/$org/get-started/<key>, with the keys below.
const stepKeys = ['organization', 'code', 'factory', 'line', 'budget', 'intake', 'people', 'first-item'] as const;

export type OnboardingStepKey = (typeof stepKeys)[number];

/** The steps with a URL under an organization: every one but the first. */
export type OrgOnboardingStepKey = Exclude<OnboardingStepKey, 'organization'>;

export const onboardingSteps: readonly { key: OnboardingStepKey; label: string }[] = [
  { key: 'organization', label: 'Organization' },
  { key: 'code', label: 'Code' },
  { key: 'factory', label: 'Factory' },
  { key: 'line', label: 'Line' },
  { key: 'budget', label: 'Budget' },
  { key: 'intake', label: 'Intake' },
  { key: 'people', label: 'People' },
  { key: 'first-item', label: 'First item' },
];

export function isOrgOnboardingStep(key: string): key is OrgOnboardingStepKey {
  return key !== 'organization' && (stepKeys as readonly string[]).includes(key);
}

export function stepIndex(step: OnboardingStepKey): number {
  return onboardingSteps.findIndex((s) => s.key === step);
}

/** The step before `step`. Step 1 has none, so the caller never asks for it. */
export function previousStep(step: OrgOnboardingStepKey): OnboardingStepKey {
  return onboardingSteps[stepIndex(step) - 1]?.key ?? 'organization';
}

export function stepLabel(step: OnboardingStepKey): string {
  return onboardingSteps[stepIndex(step)]?.label ?? '';
}
