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
import {
  getDraft,
  getDraftFile,
  getDraftModel,
  getFactory,
  getVersion,
  getVersionFile,
  getVersionModel,
  listDraftFiles,
  listTemplates,
  openDraft,
  putDraftFile,
  validateDraft,
} from '../../api/definitions';
import type { OrgSummary, TemplateSummary } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { BudgetStep } from './budget-step';
import { onboardingSearch } from './code-search';
import { CodeStep } from './code-step';
import { FirstItemStep } from './first-item-step';
import { IntakeStep } from './intake-step';
import { LineStep } from './line-step';
import { PeopleStep } from './people-step';

vi.mock('../../api/definitions', async (original) => ({
  ...(await original<typeof import('../../api/definitions')>()),
  listTemplates: vi.fn(),
  openDraft: vi.fn(),
  getDraftFile: vi.fn(),
  getDraftModel: vi.fn(),
  getFactory: vi.fn(),
  putDraftFile: vi.fn(),
  getDraft: vi.fn(),
  getVersion: vi.fn(),
  getVersionFile: vi.fn(),
  getVersionModel: vi.fn(),
  listDraftFiles: vi.fn(),
  validateDraft: vi.fn(),
}));

const factory = '6f1d2c3a-4b5e-4f70-8a9b-0c1d2e3f4a5b';
const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 0, role: 'owner' };
const search = `factory=${factory}&template=software&access=write&url=https%3A%2F%2Fgithub.com%2Facme%2Fapp&secret=github-acme`;

/** Until Line, every step stays on its loading state, which already shows the Back link. */
let factoryReady = false;

beforeEach(() => {
  factoryReady = false;
  vi.mocked(listTemplates).mockResolvedValue([
    { name: 'software', title: 'Software', code: 'none' } as unknown as TemplateSummary,
  ]);
  vi.mocked(openDraft).mockResolvedValue({} as never);
  vi.mocked(getDraftFile).mockResolvedValue({ text: 'apiVersion: goal/v1\nkind: Factory\nspec: {}\n', sha256: 'abc' });
  vi.mocked(putDraftFile).mockResolvedValue({} as never);
  vi.mocked(getFactory).mockImplementation(async () =>
    factoryReady ? ({ id: factory, name: 'Acme delivery' } as never) : new Promise<never>(() => {}),
  );
  const model = {
    basics: { name: 'software', purpose: 'Builds.' },
    line: { stations: [{ id: 'build', name: 'Build', kind: 'agent', agent: 'builder' }] },
    agents: { agents: [{ name: 'builder', model: 'claude-opus-5-5' }] },
    connections: [],
    review: {},
  };
  const file = { text: 'apiVersion: goal/v1\nkind: Factory\nspec: {}\n', sha256: 's1' };
  vi.mocked(getDraftModel).mockResolvedValue({
    model,
    etags: { basics: 'b1', line: 'l1', agents: 'a1', connections: 'c1', review: 'r1' },
    revision: 1,
  } as never);
  vi.mocked(getVersionModel).mockResolvedValue({ model, ranges: {} } as never);
  vi.mocked(getVersion).mockResolvedValue({ files: [{ path: 'factory.yaml', sha256: 's1', size: 10 }] } as never);
  vi.mocked(getVersionFile).mockResolvedValue(file as never);
  vi.mocked(getDraft).mockResolvedValue({ base: null, revision: 1 } as never);
  vi.mocked(listDraftFiles).mockResolvedValue([{ path: 'factory.yaml', sha256: 's1', size: 10 }] as never);
  vi.mocked(validateDraft).mockResolvedValue({ ok: true, revision: 1, counts: {}, checks: [], problems: [] } as never);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderWalk(url: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: Outlet });
  const step = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/get-started/$step',
    validateSearch: (s: Record<string, unknown>) => onboardingSearch(s),
    component: function Page() {
      const s = step.useSearch();
      const { step: key } = step.useParams();
      switch (key) {
        case 'code':
          return <CodeStep org={org} search={s} />;
        case 'line':
          return <LineStep org={org} search={s} />;
        case 'budget':
          return <BudgetStep org={org} search={s} />;
        case 'people':
          return <PeopleStep org={org} search={s} />;
        case 'intake':
          return <IntakeStep org={org} search={s} />;
        default:
          return <FirstItemStep org={org} search={s} />;
      }
    },
  });
  const router = createRouter({
    routeTree: root.addChildren([step]),
    history: createMemoryHistory({ initialEntries: [url] }),
  });
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { router, queryClient };
}

describe('the code choice across the onboarding steps', () => {
  it('keeps Read and write when Back is followed from First item to Code', async () => {
    const { router, queryClient } = renderWalk(`/o/acme/get-started/first-item?${search}`);
    // From the last step down: each Back leads one step down and keeps the whole search.
    const steps = ['first-item', 'people', 'intake', 'budget', 'line', 'code'];
    for (const previous of steps.slice(1)) {
      // The link can be replaced as the step's data arrives, so find it again until the click takes.
      if (previous === 'line') {
        // Line and Code read the draft, so the factory answers from here on.
        factoryReady = true;
        await queryClient.cancelQueries();
        queryClient.clear();
      }
      await waitFor(() => {
        const link = screen.getByRole('link', { name: 'Back' });
        expect(link.getAttribute('href')).toContain('access=write');
        expect(link.getAttribute('href')).toContain(`/get-started/${previous}`);
        fireEvent.click(link);
        expect(router.state.location.pathname).toBe(`/o/acme/get-started/${previous}`);
      });
    }
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/code'));
    expect(router.state.location.search).toMatchObject({ access: 'write', template: 'software', factory });
    const write = await screen.findByRole('radio', { name: /^Read and write/ });
    expect((write as HTMLInputElement).checked).toBe(true);
  });
});
