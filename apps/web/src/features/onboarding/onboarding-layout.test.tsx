// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, render, screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import type { OrgSummary } from '../../api/types';
import { OnboardingLayout } from './onboarding-layout';
import { isOrgOnboardingStep, previousStep } from './steps';

const goal: OrgSummary = { id: 'org-goal', slug: 'goal', name: 'Goal', mark: 'Go', factory_count: 1, role: 'owner' };

/** The layout inside a router: its links need one. */
async function renderLayout(element: ReactNode) {
  const queryClient = new QueryClient();
  const root = createRootRoute({ component: Outlet });
  const page = createRoute({ getParentRoute: () => root, path: '/', component: () => element });
  const org = createRoute({ getParentRoute: () => root, path: '/o/$org', component: () => <p>Home</p> });
  const router = createRouter({
    routeTree: root.addChildren([page, org]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await router.load();
  await screen.findByRole('main');
}

afterEach(() => {
  cleanup();
});

describe('the onboarding layout', () => {
  it('shows the eight steps and marks the current one', async () => {
    await renderLayout(<OnboardingLayout step="factory" org={goal} />);

    const steps = screen.getByRole('list', { name: 'Setup, step 3 of 8' });
    expect(
      within(steps)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Organization', 'Code', 'Factory', 'Line', 'Budget', 'Intake', 'People', 'First item']);
    const current = within(steps)
      .getAllByRole('listitem')
      .filter((li) => li.getAttribute('aria-current') === 'step');
    expect(current.map((li) => li.textContent)).toEqual(['Factory']);
  });

  it('shows the Goal mark and name at the top, and "Save and finish later" to the organization home', async () => {
    await renderLayout(<OnboardingLayout step="code" org={goal} />);

    expect(screen.getByText('Goal', { selector: 'span' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Save and finish later' }).getAttribute('href')).toBe('/o/goal');
  });

  it('shows "Save and finish later" as a button without an organization, since it signs out', async () => {
    await renderLayout(<OnboardingLayout step="organization" />);

    expect(screen.getByRole('button', { name: 'Save and finish later' })).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Save and finish later' })).toBeNull();
  });

  it('puts the form, Back and the primary action in the main column', async () => {
    await renderLayout(
      <OnboardingLayout
        step="code"
        org={goal}
        back={<a href="/o/goal/get-started/x">Back</a>}
        action={<button type="button">Continue</button>}
      >
        <p>The form</p>
      </OnboardingLayout>,
    );

    const main = screen.getByRole('main');
    expect(within(main).getByText('The form')).toBeTruthy();
    expect(within(main).getByRole('link', { name: 'Back' })).toBeTruthy();
    expect(within(main).getByRole('button', { name: 'Continue' })).toBeTruthy();
  });

  it('shows the panel with its placeholders until a step fills a slot', async () => {
    await renderLayout(<OnboardingLayout step="code" org={goal} />);
    const panel = screen.getByRole('complementary', { name: 'Preview of your factory' });
    expect(within(panel).getByRole('heading', { name: 'Your factory, as you build it' })).toBeTruthy();
    expect(within(panel).getByText('Updates as you go')).toBeTruthy();
    expect(within(panel).getByText('The line appears when you pick a template')).toBeTruthy();
    cleanup();

    await renderLayout(<OnboardingLayout step="line" org={goal} preview={{ line: <p>Line: Build, Test, Ship</p> }} />);
    const filled = screen.getByRole('complementary', { name: 'Preview of your factory' });
    expect(within(filled).getByText('Line: Build, Test, Ship')).toBeTruthy();
    expect(within(filled).queryByText('The line appears when you pick a template')).toBeNull();
  });
});

describe('the onboarding steps', () => {
  it('has the step keys of ADR 0032 under an organization, and not the first step', () => {
    for (const key of ['code', 'factory', 'line', 'budget', 'intake', 'people', 'first-item']) {
      expect(isOrgOnboardingStep(key)).toBe(true);
    }
    expect(isOrgOnboardingStep('organization')).toBe(false);
    expect(isOrgOnboardingStep('sign-up')).toBe(false);
  });

  it('names the step before each one, and the first step before the second', () => {
    expect(previousStep('code')).toBe('organization');
    expect(previousStep('factory')).toBe('code');
  });
});
