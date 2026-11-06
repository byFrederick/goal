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
import { getDraftFile, listTemplates, openDraft, putDraftFile } from '../../api/definitions';
import { testFactoryRepository } from '../../api/repositories';
import { createSecret, getSecret, patchSecret, replaceSecretValue } from '../../api/secrets';
import type { FactoryRepositoryTest, OrgSummary, TemplateCode, TemplateSummary } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { onboardingSearch } from './code-search';
import { CodeStep } from './code-step';

vi.mock('../../api/secrets', async (original) => ({
  ...(await original<typeof import('../../api/secrets')>()),
  createSecret: vi.fn(),
  getSecret: vi.fn(),
  patchSecret: vi.fn(),
  replaceSecretValue: vi.fn(),
}));
vi.mock('../../api/repositories', async (original) => ({
  ...(await original<typeof import('../../api/repositories')>()),
  testFactoryRepository: vi.fn(),
}));
vi.mock('../../api/definitions', async (original) => ({
  ...(await original<typeof import('../../api/definitions')>()),
  listTemplates: vi.fn(),
  openDraft: vi.fn(),
  getDraftFile: vi.fn(),
  putDraftFile: vi.fn(),
}));

const factory = '6f1d2c3a-4b5e-4f70-8a9b-0c1d2e3f4a5b';
const ok = { ok: true, reason: 'ok' };
const answer = (url: string, over: Partial<FactoryRepositoryTest> = {}): FactoryRepositoryTest =>
  ({
    url,
    access: 'write',
    secret: 'github-acme',
    at: '2026-11-05T10:00:00Z',
    tested: true,
    ok: true,
    summary: 'Can read and push',
    read: ok,
    push: ok,
    ...over,
  }) as FactoryRepositoryTest;

const factoryYaml = `apiVersion: goal/v1
kind: Factory
spec:
  runtime:
    repository:
      url: https://github.com/acme/old
    budget:
      perItemUsd: 5
  agents: []
`;

let templateCode: TemplateCode = 'write';

beforeEach(() => {
  templateCode = 'write';
  vi.mocked(listTemplates).mockImplementation(async () => [
    { name: 'software', title: 'Software', code: templateCode } as unknown as TemplateSummary,
  ]);
  vi.mocked(createSecret).mockResolvedValue({} as never);
  vi.mocked(getSecret).mockResolvedValue({ scope: { all_factories: false, factories: ['other'] } } as never);
  vi.mocked(patchSecret).mockResolvedValue({} as never);
  vi.mocked(testFactoryRepository).mockImplementation(async (_o, _f, req) => answer(req.url, { access: req.access }));
  vi.mocked(openDraft).mockResolvedValue({} as never);
  vi.mocked(getDraftFile).mockResolvedValue({ text: factoryYaml, sha256: 'abc' });
  vi.mocked(putDraftFile).mockResolvedValue({} as never);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 0, role: 'owner' };

/** The Code step at /o/acme/get-started/code for the factory; the line step is the next page. */
function renderStep(url = `/o/acme/get-started/code?factory=${factory}&template=software`) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: Outlet });
  const step = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/get-started/$step',
    validateSearch: (search: Record<string, unknown>) => onboardingSearch(search),
    component: function Step() {
      const search = step.useSearch();
      const { step: key } = step.useParams();
      return key === 'code' ? <CodeStep org={org} search={search} /> : <p>{`Step ${key}`}</p>;
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
const next = () => screen.getByRole('button', { name: /Continue|Testing/ }) as HTMLButtonElement;
const radio = async (name: RegExp) => (await screen.findByRole('radio', { name })) as HTMLInputElement;

describe('Step 3, Connect your code: the choices', () => {
  it('shows a skeleton while the template loads', async () => {
    vi.mocked(listTemplates).mockReturnValue(new Promise(() => {}));
    renderStep();
    expect(await screen.findByText('Loading')).toBeTruthy();
    expect(screen.queryByRole('radio')).toBeNull();
  });

  it('leaves only Read and write for a line that merges, with the reason behind an ⓘ', async () => {
    templateCode = 'write';
    renderStep();
    expect((await radio(/^No code/)).disabled).toBe(true);
    expect((await radio(/^Read only/)).disabled).toBe(true);
    expect((await radio(/^Read and write/)).disabled).toBe(false);
    expect((await radio(/^Read and write/)).checked).toBe(true);
    expect(screen.getAllByText('This line merges pull requests, so it needs Read and write.')).toHaveLength(2);
    // On a phone the ⓘ is 44 px.
    for (const tip of screen.getAllByRole('button', { name: /^About .* being unavailable$/ })) {
      expect(tip.className).toContain('max-md:size-11');
    }
  });

  it('disables only No code for a line that reads', async () => {
    templateCode = 'read';
    renderStep();
    expect((await radio(/^No code/)).disabled).toBe(true);
    expect((await radio(/^Read only/)).disabled).toBe(false);
    expect((await radio(/^Read and write/)).disabled).toBe(false);
    expect((await radio(/^Read only/)).checked).toBe(true);
  });

  it('disables nothing for a line with no code, shows no fields, and continues at once', async () => {
    templateCode = 'none';
    const router = renderStep();
    for (const name of [/^No code/, /^Read only/, /^Read and write/]) expect((await radio(name)).disabled).toBe(false);
    expect(screen.queryByLabelText('Token')).toBeNull();
    expect(next().disabled).toBe(false);
    fireEvent.click(next());
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/line'));
    expect(createSecret).not.toHaveBeenCalled();
    expect(testFactoryRepository).not.toHaveBeenCalled();
    expect(putDraftFile).not.toHaveBeenCalled();
    expect(router.state.location.search).toMatchObject({ factory, template: 'software' });
  });

  it('shows the same form for Read only and Read and write', async () => {
    templateCode = 'none';
    renderStep();
    for (const name of [/^Read only/, /^Read and write/]) {
      fireEvent.click(await radio(name));
      expect(await screen.findByLabelText('Repository')).toBeTruthy();
      expect(screen.getByLabelText('Token')).toBeTruthy();
      expect(screen.getByRole('radio', { name: 'Only some' })).toBeTruthy();
    }
  });

  it('adds and removes repositories in place', async () => {
    renderStep();
    await screen.findByLabelText('Repository');
    fireEvent.click(screen.getByRole('button', { name: 'Add another repository' }));
    expect(screen.getByLabelText('Repository 2')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Remove repository 2' }));
    expect(screen.queryByLabelText('Repository 2')).toBeNull();
  });

  it('shows a message beside the URL when it is not an https address', async () => {
    renderStep();
    const url = await screen.findByLabelText('Repository');
    fireEvent.change(url, { target: { value: 'github.com/acme/app' } });
    fireEvent.blur(url);
    expect(await screen.findByText(/https address/)).toBeTruthy();
    expect(next().getAttribute('aria-disabled')).toBe('true');
  });
});

describe('Step 3, Connect your code: saving and testing', () => {
  it('saves the token for this factory, tests each URL on the factory route, then writes the entries', async () => {
    const router = renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    fireEvent.click(next());

    expect(await screen.findByText('acme/app: Can read and push')).toBeTruthy();
    expect(createSecret).toHaveBeenCalledWith(
      'org-1',
      expect.objectContaining({
        name: 'github-acme',
        value: 'test-token',
        scope: { all_factories: false, factories: [factory] },
      }),
    );
    expect(testFactoryRepository).toHaveBeenCalledWith('org-1', factory, {
      url: 'https://github.com/acme/app',
      secret: 'github-acme',
      access: 'write',
    });
    expect(router.state.location.pathname).toBe('/o/acme/get-started/code');
    expect(putDraftFile).not.toHaveBeenCalled();

    fireEvent.click(next());
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/line'));
    const [, , path, text, base] = vi.mocked(putDraftFile).mock.calls[0] ?? [];
    expect(path).toBe('factory.yaml');
    expect(base).toEqual({ sha256: 'abc' });
    expect(text).toContain('name: app');
    expect(text).toContain('url: https://github.com/acme/app');
    expect(text).toContain('token: "{{github-acme}}"');
    expect(text).toContain('access: write');
    expect(text).toContain('default: true');
    expect(text).not.toContain('repository:');
    expect(router.state.location.search).toMatchObject({ factory, secret: 'github-acme' });
    expect(JSON.stringify(router.state.location)).not.toContain('test-token');
  });

  it('tests every repository and writes one entry per URL, with no default', async () => {
    templateCode = 'read';
    renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    fireEvent.click(screen.getByRole('button', { name: 'Add another repository' }));
    fireEvent.change(screen.getByLabelText('Repository 2'), { target: { value: 'https://github.com/acme/web' } });
    fireEvent.click(next());
    await screen.findByText('acme/web: Can read and push');
    expect(testFactoryRepository).toHaveBeenCalledTimes(2);
    expect(vi.mocked(testFactoryRepository).mock.calls[0]?.[2].access).toBe('read');
    fireEvent.click(next());
    await waitFor(() => expect(putDraftFile).toHaveBeenCalled());
    const text = vi.mocked(putDraftFile).mock.calls[0]?.[3] as string;
    expect(text).toContain('name: app');
    expect(text).toContain('name: web');
    expect(text).toContain('access: read');
    expect(text).not.toContain('default:');
  });

  it('covers a whole organization with one owner pattern', async () => {
    renderStep();
    fireEvent.click(await screen.findByRole('radio', { name: 'A whole organization' }));
    fireEvent.change(await screen.findByLabelText('Organization'), { target: { value: 'https://github.com/acme' } });
    fireEvent.change(screen.getByLabelText('Token'), { target: { value: 'test-token' } });
    fireEvent.click(next());
    await screen.findByText('acme/*: Can read and push');
    expect(testFactoryRepository).toHaveBeenCalledWith('org-1', factory, {
      url: 'https://github.com/acme/*',
      secret: 'github-acme',
      access: 'write',
    });
    fireEvent.click(next());
    await waitFor(() => expect(putDraftFile).toHaveBeenCalled());
    const text = vi.mocked(putDraftFile).mock.calls[0]?.[3] as string;
    expect(text).toContain('url: https://github.com/acme/*');
    expect(text).not.toContain('default:');
  });

  it('offers the existing secret or a new name on a name clash, and never replaces the value', async () => {
    vi.mocked(createSecret).mockRejectedValueOnce(
      new ApiError({ title: 'Taken', status: 409, code: 'secret-name-taken' } as ConstructorParameters<
        typeof ApiError
      >[0]),
    );
    renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    fireEvent.click(next());
    expect(await screen.findByText(/A secret named github-acme already exists/)).toBeTruthy();
    expect(testFactoryRepository).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Use the existing secret' }));
    expect(await screen.findByText('acme/app: Can read and push')).toBeTruthy();
    expect(patchSecret).toHaveBeenCalledWith('org-1', 'github-acme', {
      scope: { all_factories: false, factories: ['other', factory] },
    });
    expect(replaceSecretValue).not.toHaveBeenCalled();
  });

  it('creates the token under a new name after a clash', async () => {
    vi.mocked(createSecret).mockRejectedValueOnce(
      new ApiError({ title: 'Taken', status: 409, code: 'secret-name-taken' } as ConstructorParameters<
        typeof ApiError
      >[0]),
    );
    renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    fireEvent.click(next());
    await screen.findByText(/already exists/);
    fireEvent.change(screen.getByLabelText('New secret name'), { target: { value: 'github-acme-2' } });
    fireEvent.click(screen.getByRole('button', { name: 'Use this name' }));
    await screen.findByText('acme/app: Can read and push');
    expect(vi.mocked(createSecret).mock.calls[1]?.[1]).toMatchObject({ name: 'github-acme-2', value: 'test-token' });
    expect(replaceSecretValue).not.toHaveBeenCalled();
  });

  it('shows the host’s reason when the token cannot push, and keeps Continue disabled until it changes', async () => {
    vi.mocked(testFactoryRepository).mockImplementation(async (_o, _f, req) =>
      answer(req.url, {
        ok: false,
        summary: 'Can read but not push.',
        push: { ok: false, reason: 'The token has no write access to this repository.' },
      }),
    );
    renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    fireEvent.click(next());
    expect(await screen.findByText(/Can read but not push\. The token has no write access/)).toBeTruthy();
    expect(next().getAttribute('aria-disabled')).toBe('true');
    fireEvent.change(screen.getByLabelText('Token'), { target: { value: 'another-token' } });
    expect(next().getAttribute('aria-disabled')).not.toBe('true');
  });

  it('keeps focus on Continue when the test answers, and has the status region in the page before', async () => {
    renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    expect(screen.getByRole('status')).toBeTruthy();
    next().focus();
    fireEvent.click(next());
    expect(await screen.findByText('acme/app: Can read and push')).toBeTruthy();
    expect(document.activeElement).toBe(next());
    expect(next()).not.toHaveAttribute('disabled');
    expect(screen.getByRole('status').textContent).toContain('Can read and push');
  });

  it('keeps focus on Continue after a token that cannot push, and does not submit again', async () => {
    vi.mocked(testFactoryRepository).mockImplementation(async (_o, _f, req) =>
      answer(req.url, { ok: false, summary: 'Can read but not push.', push: { ok: false, reason: 'No write.' } }),
    );
    renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    next().focus();
    fireEvent.click(next());
    expect(await screen.findByText(/Can read but not push/)).toBeTruthy();
    expect(document.activeElement).toBe(next());
    expect(next()).not.toHaveAttribute('disabled');
    fireEvent.click(next());
    expect(testFactoryRepository).toHaveBeenCalledTimes(1);
    expect(createSecret).toHaveBeenCalledTimes(1);
  });

  it('keeps focus on Continue after a Read only result', async () => {
    templateCode = 'read';
    renderStep();
    fireEvent.click(await radio(/^Read only/));
    await fill('https://github.com/acme/app', 'test-token');
    next().focus();
    fireEvent.click(next());
    expect(await screen.findByText('acme/app: Can read and push')).toBeTruthy();
    expect(document.activeElement).toBe(next());
    expect(next()).not.toHaveAttribute('disabled');
  });

  it('starts on the access in the URL when the template allows it, and passes it on to the next step', async () => {
    templateCode = 'none';
    const router = renderStep(`/o/acme/get-started/code?factory=${factory}&template=software&access=read`);
    expect((await radio(/^Read only/)).checked).toBe(true);
    await fill('https://github.com/acme/app', 'test-token');
    fireEvent.click(next());
    expect(await screen.findByText(/acme\/app:/)).toBeTruthy();
    fireEvent.click(next());
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/line'));
    expect(router.state.location.search).toMatchObject({ access: 'read' });
  });

  it('falls back to the template’s start when the URL’s access is not allowed', async () => {
    renderStep(`/o/acme/get-started/code?factory=${factory}&template=software&access=none`);
    expect((await radio(/^Read and write/)).checked).toBe(true);
  });

  it('shows the warning of a read entry whose token can also push', async () => {
    templateCode = 'read';
    vi.mocked(testFactoryRepository).mockImplementation(async (_o, _f, req) =>
      answer(req.url, { summary: 'Can read', warning: 'Goal won’t push, but agents could. Use a read-only token.' }),
    );
    renderStep();
    await fill('https://github.com/acme/app', 'test-token');
    fireEvent.click(next());
    expect(await screen.findByText(/Use a read-only token/)).toBeTruthy();
  });

  it('removes earlier entries when No code is chosen after code was saved', async () => {
    templateCode = 'none';
    vi.mocked(getDraftFile).mockResolvedValue({
      text: factoryYaml.replace('repository:', 'repositories:\n      - name: old\n    repository:'),
      sha256: 'abc',
    });
    const router = renderStep(
      `/o/acme/get-started/code?factory=${factory}&template=software&url=https://github.com/acme/app&secret=github-acme`,
    );
    fireEvent.click(await radio(/^No code/));
    fireEvent.click(next());
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/line'));
    expect(vi.mocked(putDraftFile).mock.calls[0]?.[3]).not.toContain('repositor');
  });
});
