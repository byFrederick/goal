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
import { ApiError } from '../../api/client';
import { publishDraft } from '../../api/definitions';
import type { Factory, OrgSummary, SectionSave } from '../../api/types';
import { factoryModelOf, type Model, type Section } from '../../shared/definition/model';
import { OrgContext } from '../../shared/org';
import { TooltipProvider } from '../../ui/tooltip';
import { BudgetStep } from './budget-step';
import { onboardingSearch } from './code-search';
import { LineStep } from './line-step';

// Goald answers the factory, its draft and the publish; the step reads them through the API modules. `api` is
// what each test sets: the draft's sections and the saves the step sent.
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
  publishDraft: vi.fn(async () => ({ id: 'version-1', number: 1 }) as never),
}));

vi.mock(import('../../api/builder'), async (original) => ({
  ...(await original()),
  getBuilderMeta: vi.fn(async () => ({
    models: [{ id: 'claude-opus-5-5', label: 'Opus 5.5', default: true }],
    capabilities: { tool_approvals: false },
    limits: { skill_bytes: 0, skill_files: 0 },
  })),
}));

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 0, role: 'owner' };
const factoryId = '6f1d2c3a-4b5e-4f70-8a9b-0c1d2e3f4a5b';

function setFactory() {
  api.factory = { id: factoryId, name: 'software', draft: { revision: 1 }, current: null } as unknown as Factory;
}

/** A draft with two stations and no budget or targets: the picture's defaults show until they are set. */
function setDraft(over: Partial<Model> = {}) {
  const model: Model = {
    basics: { name: 'software', purpose: 'Builds the product.' },
    line: {
      stations: [
        { id: 'intake', name: 'Intake', kind: 'queue' },
        { id: 'build', name: 'Build', kind: 'agent', agent: 'backend-engineer', wip: 6 },
        { id: 'done', name: 'Done', kind: 'done' },
      ],
    },
    agents: { agents: [{ name: 'backend-engineer' }] },
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

/** Step 5 at /o/acme/get-started/budget with the URL's `search`; the other steps are stubs the links reach. */
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
      if (key === 'budget') return <BudgetStep org={org} search={search} />;
      if (key === 'line') return <LineStep org={org} search={search} />;
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

/** The last saved value of a draft section. */
function lastSaved<T>(section: Section): T {
  const saves = api.saves.filter((s) => s.section === section);
  return saves.at(-1)?.value as T;
}

describe('step 5, set budgets and targets', () => {
  it('sends the person back to step 3 when the URL has no factory', async () => {
    renderStep('/o/acme/get-started/budget');
    expect(await screen.findByText('Choose a template first, then set its budgets.')).toBeTruthy();
    const back = screen.getByRole('link', { name: 'Back' });
    expect(back.getAttribute('href')).toContain('/o/acme/get-started/factory');
    expect(back.getAttribute('href')).not.toContain('factory=');
  });

  it('shows the loading state while the factory is read', async () => {
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    expect((await screen.findByRole('status')).textContent).toBe('Loading');
  });

  it('shows the three sliders with their values and hints, then the estimate and the choices', async () => {
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    expect(await screen.findByRole('heading', { level: 1, name: 'Set budgets and targets' })).toBeTruthy();
    expect(
      screen.getByText('Goal stops starting new work when a budget is spent, so a busy day never surprises you.'),
    ).toBeTruthy();

    const day = screen.getByRole('slider', { name: 'A day' }) as HTMLInputElement;
    expect(day.min).toBe('10');
    expect(day.max).toBe('500');
    expect(day.value).toBe('150');
    expect(screen.getByText('fits about 48 items')).toBeTruthy();

    expect(screen.getByText('covers 5 full days')).toBeTruthy();
    expect(screen.getByText('4 times the average')).toBeTruthy();
    expect((screen.getByRole('slider', { name: 'Most per item' }) as HTMLInputElement).value).toBe('12');

    expect(screen.getByText('~$31')).toBeTruthy();
    expect(screen.getByText(/at about \$3\.10 each for a typical item/)).toBeTruthy();

    const exhausted = screen.getByRole('group', { name: 'When a budget runs out' });
    expect(within(exhausted).getByRole('radio', { name: /Finish what's started/ })).toBeTruthy();
    expect(within(exhausted).getByRole('radio', { name: /Stop everything/ })).toBeTruthy();

    expect(screen.getByRole('textbox', { name: 'Target, items a day' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'Target lead time' })).toBeTruthy();
    expect(screen.getByText('hours')).toBeTruthy();
  });

  it('writes a slider’s value to the draft and shows it on the Budget card', async () => {
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    const day = (await screen.findByRole('slider', { name: 'A day' })) as HTMLInputElement;
    fireEvent.change(day, { target: { value: '200' } });
    fireEvent.blur(day);
    await waitFor(() => expect(lastSaved<{ budget?: { daily_usd?: number } }>('review').budget?.daily_usd).toBe(200));
    expect(screen.getByText('$200 a day, $800 a week')).toBeTruthy();
    expect(screen.getByText('fits about 65 items')).toBeTruthy();
  });

  it('writes the exhaustion choice, the weekly and per-item budgets and both targets to the draft', async () => {
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    const week = (await screen.findByRole('slider', { name: 'A week' })) as HTMLInputElement;
    fireEvent.change(week, { target: { value: '1000' } });
    fireEvent.blur(week);
    await waitFor(() =>
      expect(lastSaved<{ budget?: { weekly_usd?: number } }>('review').budget?.weekly_usd).toBe(1000),
    );

    const perItem = screen.getByRole('slider', { name: 'Most per item' });
    fireEvent.change(perItem, { target: { value: '20' } });
    fireEvent.blur(perItem);
    await waitFor(() =>
      expect(lastSaved<{ budget?: { per_item_usd?: number } }>('review').budget?.per_item_usd).toBe(20),
    );

    fireEvent.click(screen.getByRole('radio', { name: /Stop everything/ }));
    await waitFor(() =>
      expect(lastSaved<{ budget?: { on_exhausted?: string } }>('review').budget?.on_exhausted).toBe('stop'),
    );

    const items = screen.getByRole('textbox', { name: 'Target, items a day' });
    fireEvent.change(items, { target: { value: '12' } });
    fireEvent.blur(items);
    await waitFor(() =>
      expect(
        lastSaved<{ flow?: { targets?: { throughput_per_day?: number } } }>('line').flow?.targets?.throughput_per_day,
      ).toBe(12),
    );

    const lead = screen.getByRole('textbox', { name: 'Target lead time' });
    fireEvent.change(lead, { target: { value: '36' } });
    fireEvent.blur(lead);
    await waitFor(() =>
      expect(
        lastSaved<{ flow?: { targets?: { lead_time_hours?: number } } }>('line').flow?.targets?.lead_time_hours,
      ).toBe(36),
    );
  });

  it('continues by publishing the draft once, with the picture’s defaults filled in, then goes to intake', async () => {
    const router = renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));

    await waitFor(() => expect(publishDraft).toHaveBeenCalledTimes(1));
    expect(publishDraft).toHaveBeenCalledWith('org-1', factoryId, {
      revision: expect.any(Number),
      expected_current_version_id: null,
      start: true,
    });
    // The defaults the draft left unset are saved before it is published.
    const review = lastSaved<{ budget?: Record<string, unknown> }>('review');
    expect(review.budget).toMatchObject({ daily_usd: 150, weekly_usd: 800, per_item_usd: 12, on_exhausted: 'drain' });
    expect(lastSaved<{ flow?: { targets?: Record<string, number> } }>('line').flow?.targets).toMatchObject({
      throughput_per_day: 10,
      lead_time_hours: 48,
    });
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/intake'));
    expect(router.state.location.search).toMatchObject({ factory: factoryId });
  });

  it('shows the problems a publish refuses and stays on the step', async () => {
    vi.mocked(publishDraft).mockRejectedValueOnce(
      new ApiError({
        type: 'about:blank',
        title: 'Conflict',
        status: 409,
        code: 'definition-invalid',
        detail: 'fix the problems first',
        problems: [
          { code: 'unknown-agent', severity: 'error', message: 'Station Build names an agent that does not exist.' },
        ],
      } as never),
    );
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));

    expect(await screen.findByText(/The factory has problems, so it is not published/)).toBeTruthy();
    expect(screen.getByText('Station Build names an agent that does not exist.')).toBeTruthy();
    expect(screen.getByRole('heading', { level: 1, name: 'Set budgets and targets' })).toBeTruthy();
  });
});
