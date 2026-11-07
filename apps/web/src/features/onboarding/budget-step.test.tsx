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
import { openDraft, publishDraft, saveDraftSection } from '../../api/definitions';
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
  getVersionModel: vi.fn(async () => ({ model: api.model, ranges: {} }) as never),
  openDraft: vi.fn(async () => ({ revision: 1 }) as never),
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

/** A published factory with no draft: version 7 runs. */
function setPublished() {
  api.factory = {
    id: factoryId,
    name: 'software',
    draft: null,
    current: { id: 'version-7', number: 7 },
  } as unknown as Factory;
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
  it('keeps the code choice and repository when Continue goes to the intake step', async () => {
    const router = renderStep(
      `/o/acme/get-started/budget?factory=${factoryId}&access=write&url=https%3A%2F%2Fgithub.com%2Facme%2Fapp&secret=github-token&template=starter`,
    );
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/intake'));
    expect(router.state.location.search).toMatchObject({
      factory: factoryId,
      access: 'write',
      url: 'https://github.com/acme/app',
      secret: 'github-token',
      template: 'starter',
    });
  });

  it('keeps the code choice and repository on its Back link', async () => {
    renderStep(
      `/o/acme/get-started/budget?factory=${factoryId}&access=write&url=https%3A%2F%2Fgithub.com%2Facme%2Fapp&secret=github-token&template=starter`,
    );
    const back = await screen.findByRole('link', { name: 'Back' });
    const href = back.getAttribute('href') ?? '';
    expect(href).toContain('/get-started/line');
    for (const part of ['access=write', 'url=https', 'secret=github-token', 'template=starter', 'factory='])
      expect(href).toContain(part);
  });

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
      screen.getByText('Goal stops starting new work when the last 7 days’ spend reaches the budget.'),
    ).toBeTruthy();
    expect(screen.queryByText(/busy day/)).toBeNull();
    expect(screen.queryByRole('slider', { name: 'A day' })).toBeNull();

    const week = screen.getByRole('slider', { name: 'A week' }) as HTMLInputElement;
    expect(week.min).toBe('50');
    expect(week.max).toBe('2000');
    expect(week.step).toBe('50');
    expect(week.value).toBe('800');
    expect(screen.getByText('about $114 a day')).toBeTruthy();

    expect(screen.getByText('one agent run, 4 times the average item')).toBeTruthy();
    expect((screen.getByRole('slider', { name: 'Most per run' }) as HTMLInputElement).value).toBe('12');
    expect(screen.queryByRole('slider', { name: 'Most per item' })).toBeNull();

    expect(screen.getByText('~$31')).toBeTruthy();
    expect(screen.getByText(/at about \$3\.10 each for a typical item/)).toBeTruthy();

    const exhausted = screen.getByRole('group', { name: 'When a budget runs out' });
    expect(within(exhausted).getByRole('radio', { name: /Finish what's started/ })).toBeTruthy();
    expect(
      screen.getByText('Running items finish; nothing new starts until the last 7 days’ spend is under the budget.'),
    ).toBeTruthy();
    expect(screen.queryByText(/until tomorrow/)).toBeNull();
    const tip = within(exhausted).getByRole('button', { name: 'About when a budget runs out' });
    const described = tip.getAttribute('aria-describedby');
    expect(described).toBeTruthy();
    expect(document.getElementById(described ?? '')?.textContent).toContain('no reset at midnight');
    expect(within(exhausted).getByRole('radio', { name: /Stop everything/ })).toBeTruthy();

    expect(screen.getByRole('textbox', { name: 'Target, items a day' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'Target lead time' })).toBeTruthy();
    expect(screen.getByText('hours')).toBeTruthy();
  });

  it('shows the week’s day share and the Budget card as the week changes, and writes no daily budget', async () => {
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    const week = (await screen.findByRole('slider', { name: 'A week' })) as HTMLInputElement;
    expect(screen.getByText('$800 a week, about $114 a day')).toBeTruthy();
    fireEvent.change(week, { target: { value: '1400' } });
    fireEvent.blur(week);
    await waitFor(() =>
      expect(lastSaved<{ budget?: { weekly_usd?: number } }>('review').budget?.weekly_usd).toBe(1400),
    );
    expect(screen.getByText('about $200 a day')).toBeTruthy();
    expect(screen.getByText('$1,400 a week, about $200 a day')).toBeTruthy();
    expect(lastSaved<{ budget?: Record<string, unknown> }>('review').budget).not.toHaveProperty('daily_usd');
  });

  it('writes the exhaustion choice, the weekly and per-item budgets and both targets to the draft', async () => {
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    const week = (await screen.findByRole('slider', { name: 'A week' })) as HTMLInputElement;
    fireEvent.change(week, { target: { value: '1000' } });
    fireEvent.blur(week);
    await waitFor(() =>
      expect(lastSaved<{ budget?: { weekly_usd?: number } }>('review').budget?.weekly_usd).toBe(1000),
    );

    const perItem = screen.getByRole('slider', { name: 'Most per run' });
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
    expect(review.budget).toMatchObject({ weekly_usd: 800, per_item_usd: 12, on_exhausted: 'drain' });
    expect(review.budget).not.toHaveProperty('daily_usd');
    expect(lastSaved<{ flow?: { targets?: Record<string, number> } }>('line').flow?.targets).toMatchObject({
      throughput_per_day: 10,
      lead_time_hours: 48,
    });
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/intake'));
    expect(router.state.location.search).toMatchObject({ factory: factoryId });
  });

  it('shows Continue as busy, not disabled, while the publish waits', async () => {
    let done: (v: never) => void = () => {};
    vi.mocked(publishDraft).mockImplementationOnce(() => new Promise((r) => (done = r)));
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    const button = await screen.findByRole('button', { name: 'Continue' });
    fireEvent.click(button);

    await waitFor(() => expect(button.getAttribute('aria-busy')).toBe('true'));
    expect(button.hasAttribute('disabled')).toBe(false);
    expect(await screen.findByText('Publishing…')).toBeTruthy();
    done({ id: 'version-1', number: 1 } as never);
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

  it('explains line and agents problems in plain sentences with one link to the Line step', async () => {
    vi.mocked(publishDraft).mockRejectedValueOnce(
      new ApiError({
        type: 'about:blank',
        title: 'Conflict',
        status: 409,
        code: 'definition-invalid',
        detail: 'fix the problems first',
        problems: [
          {
            code: 'schema-required',
            severity: 'error',
            section: 'files',
            pointer: '',
            message: "spec.board: missing property 'stations'",
          },
          {
            code: 'schema-required',
            severity: 'error',
            section: 'files',
            pointer: '',
            message: "spec: missing property 'agents'",
          },
          {
            code: 'unknown-agent',
            severity: 'error',
            section: 'line',
            pointer: '/line/stations/0/agent',
            message: 'Station Build names an agent that does not exist.',
          },
        ],
      } as never),
    );
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));

    expect(await screen.findByText('The line has no stations. Add one on the Line step.')).toBeTruthy();
    expect(screen.getByText('The factory has no agents. Add one on the Line step.')).toBeTruthy();
    const links = screen.getAllByRole('link', { name: /Line step/ });
    expect(links).toHaveLength(1);
    const href = links[0]?.getAttribute('href') ?? '';
    expect(href).toContain('/o/acme/get-started/line');
    expect(href).toContain(`factory=${factoryId}`);
    expect(screen.queryByText(/spec|\/line\//)).toBeNull();
  });
});

describe('step 5 on a published factory with no draft', () => {
  beforeEach(() => {
    setPublished();
    setDraft({
      review: { budget: { weekly_usd: 600, per_item_usd: 9, on_exhausted: 'stop' } },
      line: {
        stations: [
          { id: 'build', name: 'Build', kind: 'agent', agent: 'backend-engineer', wip: 6 },
          { id: 'done', name: 'Done', kind: 'done' },
        ],
        flow: { targets: { throughput_per_day: 10, lead_time_hours: 48 } },
      },
    });
  });

  it('shows the running version’s budget, and no message about choosing a template', async () => {
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    const week = (await screen.findByRole('slider', { name: 'A week' })) as HTMLInputElement;
    expect(week.value).toBe('600');
    expect((screen.getByRole('slider', { name: 'Most per run' }) as HTMLInputElement).value).toBe('9');
    expect(screen.queryByText(/Choose a template/)).toBeNull();
    expect(openDraft).not.toHaveBeenCalled();
  });

  it('opens a draft from the version on the first change, then saves it', async () => {
    renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    const week = (await screen.findByRole('slider', { name: 'A week' })) as HTMLInputElement;
    fireEvent.change(week, { target: { value: '700' } });
    await waitFor(() =>
      expect(lastSaved<{ budget?: { weekly_usd?: number } }>('review')?.budget?.weekly_usd).toBe(700),
    );
    expect(openDraft).toHaveBeenCalledTimes(1);
    expect(vi.mocked(openDraft).mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(saveDraftSection).mock.invocationCallOrder[0] ?? 0,
    );
  });

  it('continues to intake without publishing when nothing changed', async () => {
    const router = renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/intake'));
    expect(publishDraft).not.toHaveBeenCalled();
    expect(openDraft).not.toHaveBeenCalled();
  });

  it('publishes the changed draft against the running version, then goes to intake', async () => {
    const router = renderStep(`/o/acme/get-started/budget?factory=${factoryId}`);
    const week = (await screen.findByRole('slider', { name: 'A week' })) as HTMLInputElement;
    fireEvent.change(week, { target: { value: '700' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    await waitFor(() => expect(publishDraft).toHaveBeenCalledTimes(1));
    expect(publishDraft).toHaveBeenCalledWith('org-1', factoryId, {
      revision: expect.any(Number),
      expected_current_version_id: 'version-7',
    });
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/intake'));
  });
});
