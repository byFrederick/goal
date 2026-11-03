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
import { ApiError } from '../../api/client';
import { testRepository } from '../../api/repositories';
import { createSecret, replaceSecretValue } from '../../api/secrets';
import type { OrgSummary, RepositoryTestResult } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { codeSearch } from './code-search';
import { CodeStep } from './code-step';

vi.mock('../../api/secrets', async (original) => ({
  ...(await original<typeof import('../../api/secrets')>()),
  createSecret: vi.fn(),
  replaceSecretValue: vi.fn(),
}));
vi.mock('../../api/repositories', () => ({ testRepository: vi.fn() }));

const ok = { ok: true, reason: 'ok' };
const passing: RepositoryTestResult = { tested: true, read: ok, push: ok };

beforeEach(() => {
  vi.mocked(createSecret).mockResolvedValue({} as never);
  vi.mocked(replaceSecretValue).mockResolvedValue({} as never);
  vi.mocked(testRepository).mockResolvedValue(passing);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 0, role: 'owner' };

/** Step 2 at /o/acme/get-started/code with the URL's search; the factory step is the next page. */
function renderStep(url = '/o/acme/get-started/code') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: Outlet });
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
  const start = createRoute({ getParentRoute: () => root, path: '/get-started', component: () => <p>Start</p> });
  const router = createRouter({
    routeTree: root.addChildren([step, start]),
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

const fill = async (url: string, token: string) => {
  fireEvent.change(await screen.findByLabelText('Repository'), { target: { value: url } });
  fireEvent.change(screen.getByLabelText('Token'), { target: { value: token } });
};
const next = () => screen.getByRole('button', { name: /Continue|Testing/ });

describe('Step 2, Connect your code', () => {
  it('asks for a repository URL and a write-only token, and no repository list', async () => {
    renderStep();
    expect(await screen.findByRole('heading', { level: 1, name: 'Connect your code' })).toBeTruthy();
    expect(screen.getByLabelText('Token').getAttribute('type')).toBe('password');
    expect(screen.queryByText(/connection|Goal app/i)).toBeNull();
    expect((next() as HTMLButtonElement).disabled).toBe(true);
  });

  it('saves the token as a secret, tests it, and goes on with the URL and the secret name', async () => {
    const router = renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    fireEvent.click(next());

    expect(await screen.findByText('Goal cloned acme/app and can push a branch.')).toBeTruthy();
    expect(createSecret).toHaveBeenCalledWith(
      'org-1',
      expect.objectContaining({
        name: 'github-token',
        value: 'test-token',
        scope: { all_factories: true, factories: [] },
      }),
    );
    expect(testRepository).toHaveBeenCalledWith('org-1', { url: 'https://github.com/acme/app', secret: 'github-token' });
    expect(router.state.location.pathname).toBe('/o/acme/get-started/code');

    fireEvent.click(next());
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/factory'));
    expect(router.state.location.search).toEqual({
      url: 'https://github.com/acme/app',
      secret: 'github-token',
    });
    expect(JSON.stringify(router.state.location)).not.toContain('test-token');
  });

  it('replaces the value when the secret name is already taken', async () => {
    vi.mocked(createSecret).mockRejectedValue(
      new ApiError({ title: 'Taken', status: 409, code: 'secret-name-taken' } as ConstructorParameters<typeof ApiError>[0]),
    );
    renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    fireEvent.click(next());
    await screen.findByText('Goal cloned acme/app and can push a branch.');
    expect(replaceSecretValue).toHaveBeenCalledWith('org-1', 'github-token', 'test-token');
  });

  it('shows the host’s reason when the token cannot push, and keeps Continue disabled', async () => {
    vi.mocked(testRepository).mockResolvedValue({
      tested: true,
      read: ok,
      push: { ok: false, reason: 'The token has no write access to this repository.' },
    });
    renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    fireEvent.click(next());

    expect(await screen.findByText(/can’t push a branch\. The token has no write access/)).toBeTruthy();
    expect((next() as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(screen.getByLabelText('Token'), { target: { value: 'another-token' } });
    expect((next() as HTMLButtonElement).disabled).toBe(false);
  });

  it('shows a message beside the URL when it is not an https address', async () => {
    renderStep();
    const url = await screen.findByLabelText('Repository');
    fireEvent.change(url, { target: { value: 'github.com/acme/app' } });
    fireEvent.blur(url);
    expect(await screen.findByText(/https address/)).toBeTruthy();
  });

  it('tests the saved secret from a link without asking for the token again', async () => {
    renderStep('/o/acme/get-started/code?url=https://github.com/acme/app&secret=github-token&connection=old');
    const url = (await screen.findByLabelText('Repository')) as HTMLInputElement;
    expect(url.value).toBe('https://github.com/acme/app');
    fireEvent.click(next());
    await screen.findByText('Goal cloned acme/app and can push a branch.');
    expect(createSecret).not.toHaveBeenCalled();
  });
});

describe('codeSearch', () => {
  it('keeps an https url and a secret name, and drops the rest', () => {
    expect(codeSearch({ url: 'https://github.com/acme/app', secret: 'github-token' })).toEqual({
      url: 'https://github.com/acme/app',
      secret: 'github-token',
    });
    expect(codeSearch({ url: 'http://github.com/acme/app', secret: 'Bad Name', connection: 'github', repos: ['a/b'] })).toEqual({
      url: undefined,
      secret: undefined,
    });
    expect(codeSearch({ url: 'https://user:pw@github.com/acme/app' }).url).toBeUndefined();
  });
});
