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
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { listConnectionRepositories, listConnections } from '../../api/connections';
import type { Connection, OrgSummary, Repository } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { codeSearch } from './code-search';
import { CodeStep } from './code-step';

// Goald answers the connections and the repositories; the step reads them through the API module.
// `api` is what the test sets: the connections, the list of an owner, a hold that never answers, and
// how many requests fail first.
const api = vi.hoisted(() => ({
  connections: [] as Connection[],
  hold: false,
  failures: 0,
  repositories: [] as Repository[],
}));

vi.mock(import('../../api/connections'), async (original) => ({
  ...(await original()),
  listConnections: vi.fn(async () => api.connections),
  listConnectionRepositories: vi.fn(async (_org: string, _connection: string, owner?: string) => {
    if (api.hold) return new Promise<never>(() => {});
    if (api.failures > 0) {
      api.failures -= 1;
      const { ApiError } = await import('../../api/client');
      throw new ApiError({
        title: 'Code host error',
        status: 502,
        code: 'code-host-error',
        detail: 'GitHub refused the token',
      } as ConstructorParameters<typeof ApiError>[0]);
    }
    const account = owner ?? 'acme';
    return {
      login: 'acme',
      owner: account,
      owners: [
        { login: 'acme', kind: 'user' as const },
        { login: 'acme-labs', kind: 'organization' as const },
      ],
      repositories: account === 'acme' ? api.repositories : [],
      truncated: false,
    };
  }),
}));

// Each test starts with the organization's GitHub connection and the four acme repositories.
beforeEach(() => {
  api.connections = [githubConnection];
  api.hold = false;
  api.failures = 0;
  api.repositories = repositories;
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 0, role: 'owner' };

const githubConnection = { name: 'github', kind: 'github', description: 'acme-bot' } as Connection;

const ago = (ms: number) => new Date(Date.now() - ms).toISOString();
const hour = 3_600_000;
const day = 86_400_000;

const repositories = [
  {
    full_name: 'acme/goal',
    clone_url: 'https://github.com/acme/goal.git',
    languages: ['Go', 'TypeScript'],
    updated_at: ago(2 * hour),
  },
  {
    full_name: 'acme/goal-factories',
    clone_url: 'https://github.com/acme/goal-factories.git',
    languages: ['YAML'],
    updated_at: ago(5 * hour),
  },
  {
    full_name: 'acme/site',
    clone_url: 'https://github.com/acme/site.git',
    languages: ['Astro'],
    updated_at: ago(21 * day),
  },
  {
    full_name: 'acme/dotfiles',
    clone_url: 'https://github.com/acme/dotfiles.git',
    languages: ['Shell'],
    updated_at: ago(60 * day),
  },
];

/**
 * Step 2 at /o/acme/get-started/code with the URL's `search`. The factory step is the next page, and the
 * add-connection and connection pages are there for the links. Returns the router, whose location a test reads.
 */
function renderStep(url = '/o/acme/get-started/code') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: Outlet });
  // The app's route: /o/$org/get-started/$step, the Code step with its search and the other steps after it.
  const step = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/get-started/$step',
    validateSearch: (search: Record<string, unknown>) => codeSearch(search),
    component: function Step() {
      const search = step.useSearch();
      const { step: key } = step.useParams();
      return key === 'code' ? <CodeStep org={org} search={search} /> : <p>Factory step</p>;
    },
  });
  const add = createRoute({ getParentRoute: () => root, path: '/o/$org/connections/new', component: () => <p>Add</p> });
  const connection = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/connections/$connection',
    component: () => <p>Connection page</p>,
  });
  const router = createRouter({
    routeTree: root.addChildren([step, add, connection]),
    history: createMemoryHistory({ initialEntries: [url] }),
  });
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return router;
}

/** The checkbox of the row a name is shown in: the row is a label around it, so the name is its text. */
function checkboxOf(name: HTMLElement): HTMLInputElement {
  return name.closest('label')?.querySelector('input') as HTMLInputElement;
}

/** The row's checkbox, once the list shows the name. The Code card of the panel shows a chosen name too. */
function findRow(name: string): Promise<HTMLInputElement> {
  return waitFor(() => {
    const el = screen.getAllByText(name, { exact: true }).find((e) => e.closest('label'));
    if (!el) throw new Error(`no row for ${name}`);
    return checkboxOf(el);
  });
}

/** The repository's row, by its name: the checkbox the row labels. */
function row(name: string): HTMLInputElement {
  const el = screen.getAllByText(name, { exact: true }).find((e) => e.closest('label'));
  if (!el) throw new Error(`no row for ${name}`);
  return checkboxOf(el);
}

describe('the Code step', () => {
  it('shows the account a GitHub connection signs in as, and Switch account to its page', async () => {
    renderStep();

    expect(await screen.findByText('Connected as acme')).toBeTruthy();
    expect(screen.getByText('Connected')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Switch account' }).getAttribute('href')).toBe(
      '/o/acme/connections/github',
    );
  });

  it('links a person with no GitHub connection to add one, which returns to this step', async () => {
    api.connections = [];
    renderStep();

    const link = await screen.findByRole('link', { name: 'Connect GitHub' });
    const href = new URL(link.getAttribute('href') ?? '', 'http://goal.invalid');
    expect(href.pathname).toBe('/o/acme/connections/new');
    expect(href.searchParams.get('kind')).toBe('github');
    expect(href.searchParams.get('next')).toBe('/o/acme/get-started/code');
    expect(listConnectionRepositories).not.toHaveBeenCalled();
  });

  it('says it is loading to a screen reader while the repositories load', async () => {
    api.hold = true;
    renderStep();

    const status = await screen.findByRole('status');
    expect(status.textContent).toBe('Loading');
    expect(status.parentElement?.getAttribute('aria-busy')).toBe('true');
  });

  it('says the owner has no repositories the Goal app can reach', async () => {
    api.repositories = [];
    renderStep();

    expect(await screen.findByText('acme has no repositories the Goal app can reach.')).toBeTruthy();
    expect(screen.queryByRole('checkbox')).toBeNull();
  });

  it('shows the problem the list failed with, and tries again', async () => {
    api.failures = 1;
    renderStep();

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Code host error: GitHub refused the token');
    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));

    expect(await findRow('acme/goal')).toBeTruthy();
  });

  it('shows the problem when the connections cannot be listed, not a GitHub card to connect again', async () => {
    const { ApiError } = await import('../../api/client');
    vi.mocked(listConnections).mockRejectedValueOnce(
      new ApiError({
        title: 'Service unavailable',
        status: 503,
        code: 'unavailable',
        detail: 'Connections are unavailable',
      } as ConstructorParameters<typeof ApiError>[0]),
    );
    renderStep();

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Connections are unavailable');
    expect(screen.queryByRole('link', { name: 'Connect GitHub' })).toBeNull();
    expect(screen.queryByText('Not connected yet')).toBeNull();

    api.connections = [];
    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    expect(await screen.findByRole('link', { name: 'Connect GitHub' })).toBeTruthy();
  });

  it('lists the repositories with their languages and age, and the owner with the account first', async () => {
    renderStep();

    expect(((await findRow('acme/goal')) as HTMLInputElement).checked).toBe(false);
    expect(screen.getByText('Go, TypeScript')).toBeTruthy();
    expect(screen.getAllByText('Updated today')).toHaveLength(2);
    expect(screen.getByText('Updated 3 weeks ago')).toBeTruthy();
    expect(screen.getByText('Updated 2 months ago')).toBeTruthy();

    const options = screen.getByRole('combobox', { name: 'Owner' }).querySelectorAll('option');
    expect([...options].map((o) => o.textContent)).toEqual(['acme, your account', 'acme-labs']);
  });

  it('refetches the list for the owner picked, and drops the repositories chosen under the last one', async () => {
    renderStep('/o/acme/get-started/code?repos=acme%2Fsite');
    await findRow('acme/site');

    fireEvent.change(screen.getByRole('combobox', { name: 'Owner' }), { target: { value: 'acme-labs' } });

    expect(await screen.findByText('acme-labs has no repositories the Goal app can reach.')).toBeTruthy();
    expect(listConnectionRepositories).toHaveBeenLastCalledWith('org-1', 'github', 'acme-labs', expect.anything());
    expect((screen.getByRole('button', { name: 'Continue with 0 repositories' }) as HTMLButtonElement).disabled).toBe(
      true,
    );
  });

  it('filters the list by full name as it is typed', async () => {
    renderStep();
    await findRow('acme/goal');

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search 4 repositories' }), { target: { value: 'SITE' } });

    expect(screen.getByText('acme/site')).toBeTruthy();
    expect(screen.queryByText('acme/goal')).toBeNull();
  });

  it('disables Continue until a repository is chosen, then counts them in the label', async () => {
    renderStep();
    await findRow('acme/goal');
    expect((screen.getByRole('button', { name: 'Continue with 0 repositories' }) as HTMLButtonElement).disabled).toBe(
      true,
    );

    fireEvent.click(row('acme/goal'));
    expect(
      ((await screen.findByRole('button', { name: 'Continue with 1 repository' })) as HTMLButtonElement).disabled,
    ).toBe(false);
    fireEvent.click(row('acme/site'));
    expect(
      ((await screen.findByRole('button', { name: 'Continue with 2 repositories' })) as HTMLButtonElement).disabled,
    ).toBe(false);
  });

  it('shows the first chosen repository on the Code card of the panel', async () => {
    renderStep();
    await findRow('acme/goal');

    const panel = screen.getByRole('complementary', { name: 'Preview of your factory' });
    expect(within(panel).queryByText('GitHub, read and pull requests')).toBeNull();

    fireEvent.click(row('acme/goal'));

    expect(await within(panel).findByText('acme/goal')).toBeTruthy();
    expect(within(panel).getByText('GitHub, read and pull requests')).toBeTruthy();
  });

  it('carries the chosen repositories to the next step in the URL', async () => {
    const router = renderStep();
    await findRow('acme/goal');
    fireEvent.click(row('acme/goal'));
    await screen.findByRole('button', { name: 'Continue with 1 repository' });
    fireEvent.click(row('acme/site'));

    fireEvent.click(await screen.findByRole('button', { name: 'Continue with 2 repositories' }));

    expect(await screen.findByText('Factory step')).toBeTruthy();
    expect(router.state.location.pathname).toBe('/o/acme/get-started/factory');
    expect(router.state.location.search).toMatchObject({
      connection: 'github',
      owner: 'acme',
      repos: ['acme/goal', 'acme/site'],
    });
  });

  it('keeps the repositories the URL names checked, and ignores one the list does not have', async () => {
    renderStep('/o/acme/get-started/code?repos=acme%2Fsite&repos=acme%2Fgone');

    expect(((await findRow('acme/site')) as HTMLInputElement).checked).toBe(true);
    expect((screen.getByRole('button', { name: 'Continue with 1 repository' }) as HTMLButtonElement).disabled).toBe(
      false,
    );
  });

  it('shows GitLab as not available yet, with its control disabled', async () => {
    renderStep();

    const gitlab = await screen.findByRole('button', { name: 'GitLab is coming.' });
    expect(gitlab.getAttribute('aria-disabled')).toBe('true');
  });

  it('lists what the agents read, write and never do', async () => {
    renderStep();

    expect(await screen.findByText('Code, issues, checks')).toBeTruthy();
    expect(screen.getByText('Branches and pull requests; comments only when you approve')).toBeTruthy();
    expect(screen.getByText('Force pushes, deletes or settings')).toBeTruthy();
  });
});
