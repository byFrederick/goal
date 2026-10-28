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
import type { Factory, OrgSummary, SectionSave } from '../../api/types';
import { factoryModelOf, type Model, type Section } from '../../shared/definition/model';
import { OrgContext } from '../../shared/org';
import { TooltipProvider } from '../../ui/tooltip';
import { onboardingSearch } from './code-search';
import { FactoryStep } from './factory-step';
import { LineStep } from './line-step';

// Goald answers the factory, its draft and the models; the step reads them through the API modules. `api` is
// what each test sets: the stations and agents of the draft, and the saves the step sent.
const api = vi.hoisted(() => ({
  model: {} as Record<string, unknown>,
  factory: null as unknown,
  saves: [] as { section: string; value: unknown }[],
}));

vi.mock(import('../../api/definitions'), async (original) => ({
  ...(await original()),
  getFactory: vi.fn(async () => api.factory as Factory),
  getDraft: vi.fn(async () => ({ base: null, revision: 1 }) as never),
  getDraftModel: vi.fn(
    async () =>
      ({
        model: api.model,
        etags: { basics: 'b1', line: 'l1', agents: 'a1', connections: 'c1', review: 'r1' },
        revision: 1,
      }) as never,
  ),
  saveDraftSection: vi.fn(
    async (_org: string, _factory: string, section: Section, _etag: string, save: SectionSave) => {
      api.saves.push({ section, value: save.value });
      return {
        etag: 'next',
        revision: 2,
        updated_at: '2026-10-28T10:00:00Z',
        sections: [],
        change_count: 1,
        problem_count: 0,
      };
    },
  ),
}));

vi.mock(import('../../api/builder'), async (original) => ({
  ...(await original()),
  getBuilderMeta: vi.fn(async () => ({
    models: [
      { id: 'claude-opus-5-5', label: 'Opus 5.5', default: true },
      { id: 'claude-haiku-5-5', label: 'Haiku 5.5', default: false },
    ],
    capabilities: { tool_approvals: false },
    limits: { skill_bytes: 0, skill_files: 0 },
  })),
}));

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 0, role: 'owner' };
const factoryId = '6f1d2c3a-4b5e-4f70-8a9b-0c1d2e3f4a5b';

/** A factory made from the Software template, with its draft open (the factory has a draft). */
function setFactory() {
  api.factory = { id: factoryId, name: 'software', draft: { revision: 1 }, current: null } as unknown as Factory;
}

/** The draft's sections as step 4 reads them: an intake, two agent stations with lanes and a gate, and done. */
function setDraft(over: Partial<Model> = {}) {
  const model: Model = {
    basics: { name: 'software', purpose: 'Builds the product.', repository: { url: 'https://github.com/acme/goal' } },
    line: {
      stations: [
        { id: 'intake', name: 'Intake', kind: 'queue' },
        {
          id: 'spec',
          name: 'Spec',
          kind: 'agent',
          agent: 'architect',
          agents: 3,
          wip: 6,
          description: 'Writes the spec.',
        },
        {
          id: 'build',
          name: 'Build',
          kind: 'agent',
          agent: 'backend-engineer',
          agents: 6,
          wip: 6,
          route: { backend: 'backend-engineer', frontend: 'frontend-engineer' },
        },
        {
          id: 'review',
          name: 'Review',
          kind: 'agent',
          agent: 'reviewer',
          wip: 3,
          gate: { on_reject: 'build', max_rework: 2 },
          action: 'merge',
        },
        { id: 'done', name: 'Done', kind: 'done' },
      ],
    },
    agents: {
      agents: [
        { name: 'architect', model: 'claude-opus-5-5' },
        { name: 'backend-engineer', model: 'claude-opus-5-5' },
        { name: 'reviewer' },
      ],
    },
    connections: [],
    review: {},
    ...over,
  };
  api.model = factoryModelOf(model) as unknown as Record<string, unknown>;
}

beforeEach(() => {
  api.saves = [];
  setFactory();
  setDraft();
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

/** Step 4 at /o/acme/get-started/line with the URL's `search`; the other steps are stubs the links reach. */
function renderStep(url: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: Outlet });
  const step = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/get-started/$step',
    validateSearch: (search: Record<string, unknown>) => onboardingSearch(search),
    component: function Page() {
      const search = step.useSearch();
      const { step: key } = step.useParams();
      if (key === 'line') return <LineStep org={org} search={search} />;
      if (key === 'factory') return <FactoryStep org={org} search={search} />;
      return <p>{`Step ${key}`}</p>;
    },
  });
  const router = createRouter({
    routeTree: root.addChildren([step]),
    history: createMemoryHistory({ initialEntries: [url] }),
  });
  render(
    <QueryClientProvider client={queryClient}>
      <OrgContext value={org}>
        <TooltipProvider>
          <RouterProvider router={router} />
        </TooltipProvider>
      </OrgContext>
    </QueryClientProvider>,
  );
  return router;
}

describe('step 4, shape the line', () => {
  it('sends the person back to step 3 when the URL has no factory', async () => {
    renderStep('/o/acme/get-started/line');
    expect(await screen.findByText('Choose a template first, then shape its line.')).toBeTruthy();
    const back = screen.getByRole('link', { name: 'Back' });
    expect(back.getAttribute('href')).toContain('/o/acme/get-started/factory');
    expect(back.getAttribute('href')).not.toContain('factory=');
  });

  it('shows the loading state, then each station as a card in order', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    expect((await screen.findByRole('status')).textContent).toBe('Loading');
    const stations = await screen.findByRole('list', { name: 'The stations' });
    const names = within(stations)
      .getAllByRole('region')
      .map((r) => r.getAttribute('aria-label'));
    expect(names).toEqual(['Intake', 'Spec', 'Build', 'Review', 'Done']);
  });

  it('shows a station’s copies and WIP, its lanes and its gate', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    const build = await screen.findByRole('region', { name: 'Build' });
    expect((within(build).getByRole('textbox', { name: 'copies of Build' }) as HTMLInputElement).value).toBe('6');
    expect(within(build).getByRole('textbox', { name: 'WIP limit of Build' })).toBeTruthy();
    expect(within(build).getByText('frontend-engineer')).toBeTruthy();
    const review = screen.getByRole('region', { name: 'Review' });
    expect(within(review).getByText('Approval merges; rejects go back to Build, at most 2 times.')).toBeTruthy();
  });

  it('saves a station’s agent through the draft', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    const spec = await screen.findByRole('region', { name: 'Spec' });
    fireEvent.change(within(spec).getByRole('combobox', { name: 'Agent of Spec' }), { target: { value: 'reviewer' } });
    await waitFor(() => expect(api.saves.at(-1)?.section).toBe('line'), { timeout: 3000 });
    const line = api.saves.at(-1)?.value as Model['line'];
    expect(line.stations?.find((s) => s.id === 'spec')?.agent).toBe('reviewer');
  });

  it('lists each agent with its model, and saves a change of model', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    const models = await screen.findByRole('region', { name: 'Models' });
    expect(within(models).getByText('architect')).toBeTruthy();
    const model = within(models).getByRole('combobox', { name: 'Model of architect' });
    await within(model).findByRole('option', { name: 'claude-haiku-5-5' });
    fireEvent.change(model, { target: { value: 'claude-haiku-5-5' } });
    await waitFor(() => expect(api.saves.at(-1)?.section).toBe('agents'), { timeout: 3000 });
    const agents = api.saves.at(-1)?.value as Model['agents'];
    expect(agents.agents?.find((a) => a.name === 'architect')?.model).toBe('claude-haiku-5-5');
  });

  it('keeps the quality-first and cost-first preset disabled, and never shows a copy number for an agent', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    await screen.findByRole('region', { name: 'Models' });
    const preset = screen.getByRole('button', { name: 'Not available yet' });
    expect(preset.getAttribute('aria-disabled')).toBe('true');
    expect(screen.queryByText(/copy 1|copy 2|copy 3/i)).toBeNull();
  });

  it('continues to the budget step with the factory, without publishing it', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    const next = await screen.findByRole('link', { name: 'Continue' });
    expect(next.getAttribute('href')).toBe(`/o/acme/get-started/budget?factory=${factoryId}`);
  });
});

describe('the factory card on step 4', () => {
  it('shows the factory with its station circles and WIP slots', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    const card = await screen.findByRole('region', { name: 'Your factory' });
    expect(within(card).getByText('Builds the product.')).toBeTruthy();
    expect(within(card).getByText('WIP limit 6 at Build')).toBeTruthy();
    expect(within(card).getByRole('list', { name: 'The line' }).querySelectorAll('li')).toHaveLength(5);
  });
});

describe('step 3 hands the factory on', () => {
  it('is reached from step 4 when the factory exists, so Back keeps it', async () => {
    renderStep(`/o/acme/get-started/factory?factory=${factoryId}`);
    expect(await screen.findByRole('link', { name: 'Continue' })).toBeTruthy();
  });
});
