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
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Org, OrgSummary } from '../../api/types';

const { createOrg } = vi.hoisted(() => ({ createOrg: vi.fn() }));

// The request is the only network call step 1 makes: the test stands in for goald's POST /v1/orgs.
vi.mock('../../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/client')>();
  return { ...actual, createOrg };
});

// Imported after the mock, so the ApiError the test throws is the class the step checks with instanceof.
const { ApiError } = await import('../../api/client');
const { OrgStep } = await import('./org-step');
const { getOrgUses } = await import('./org-uses');

const created = {
  id: 'org-acme',
  slug: 'acme-two',
  name: 'Acme Two',
  mark: 'AT',
  factory_count: 0,
  created_at: '2026-10-28T00:00:00Z',
  role: 'owner',
} as Org;

/** Step 1 at /get-started, inside a router that also has the next step's page and the organization's home. */
async function renderStep(home?: OrgSummary) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const root = createRootRoute({ component: Outlet });
  const start = createRoute({
    getParentRoute: () => root,
    path: '/get-started',
    component: () => <OrgStep home={home} />,
  });
  const step = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/get-started/$step',
    component: () => <p>Step page</p>,
  });
  const org = createRoute({ getParentRoute: () => root, path: '/o/$org', component: () => <p>Organization home</p> });
  const router = createRouter({
    routeTree: root.addChildren([start, step, org]),
    history: createMemoryHistory({ initialEntries: ['/get-started'] }),
  });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await router.load();
  await screen.findByRole('heading', { name: 'Name your organization' });
  return router;
}

function typeName(value: string) {
  fireEvent.change(screen.getByLabelText('Organization name'), { target: { value } });
}

function typeAddress(value: string) {
  fireEvent.change(screen.getByLabelText('Address'), { target: { value } });
}

function addressValue(): string {
  return (screen.getByLabelText('Address') as HTMLInputElement).value;
}

function continueButton() {
  return screen.getByRole('button', { name: 'Continue' });
}

beforeEach(() => {
  createOrg.mockReset();
  localStorage.clear();
});

afterEach(() => {
  cleanup();
});

describe('step 1, name the organization', () => {
  it('the address follows the name until the person edits it', async () => {
    await renderStep();
    typeName('Acme Two');
    expect(addressValue()).toBe('acme-two');

    typeAddress('acme-labs');
    typeName('Acme Three');
    expect(addressValue()).toBe('acme-labs');
  });

  it('shows the local rule on the address as it is typed, and refuses a reserved one before Continue', async () => {
    await renderStep();
    typeName('New');
    expect(addressValue()).toBe('new');
    expect(screen.getByText('That address is reserved.')).toBeTruthy();

    fireEvent.click(continueButton());
    expect(createOrg).not.toHaveBeenCalled();
  });

  it('creates the organization, keeps the chosen uses in this browser and goes to step 2', async () => {
    createOrg.mockResolvedValue(created);
    const router = await renderStep();

    typeName('Acme Two');
    fireEvent.click(screen.getByRole('button', { name: 'Run operations' }));
    expect(screen.getByRole('button', { name: 'Run operations' }).getAttribute('aria-pressed')).toBe('true');
    fireEvent.click(continueButton());

    await screen.findByText('Step page');
    expect(createOrg).toHaveBeenCalledWith({ name: 'Acme Two', slug: 'acme-two' });
    expect(router.state.location.pathname).toBe('/o/acme-two/get-started/code');
    expect(getOrgUses('org-acme')).toEqual(['build', 'operations']);
  });

  it('shows the taken message on the address and moves focus there when goald says it is taken', async () => {
    createOrg.mockRejectedValue(new ApiError({ title: 'Conflict', status: 409, code: 'slug-taken' }));
    await renderStep();

    typeName('Acme Two');
    fireEvent.click(continueButton());

    await screen.findByText('acme-two is taken. Choose another address.');
    expect(document.activeElement).toBe(screen.getByLabelText('Address'));
  });

  it('shows any other failure with Retry, and Retry sends the same request again', async () => {
    createOrg
      .mockRejectedValueOnce(new ApiError({ title: 'Server error', status: 500 }))
      .mockResolvedValueOnce(created);
    await renderStep();

    typeName('Acme Two');
    fireEvent.click(continueButton());
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Could not create the organization');

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await screen.findByText('Step page');
    expect(createOrg).toHaveBeenCalledTimes(2);
    expect(createOrg).toHaveBeenLastCalledWith({ name: 'Acme Two', slug: 'acme-two' });
  });

  it('marks Continue busy while the request runs', async () => {
    createOrg.mockReturnValue(new Promise(() => {}));
    await renderStep();

    typeName('Acme Two');
    fireEvent.click(continueButton());

    await waitFor(() => expect(continueButton().getAttribute('aria-busy')).toBe('true'));
  });

  it('Back and Save and finish later go to the organization the person is in', async () => {
    await renderStep({ id: 'org-goal', slug: 'goal', name: 'Goal', mark: 'Go', factory_count: 1, role: 'owner' });
    expect(screen.getByRole('link', { name: 'Back' }).getAttribute('href')).toBe('/o/goal');
    expect(screen.getByRole('link', { name: 'Save and finish later' }).getAttribute('href')).toBe('/o/goal');
  });

  it('shows no Back link when the person has no organization yet', async () => {
    await renderStep();
    expect(screen.queryByRole('link', { name: 'Back' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Save and finish later' })).toBeTruthy();
  });
});
