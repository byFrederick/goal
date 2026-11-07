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
import { openDraft, saveDraftSection } from '../../api/definitions';
import { createSecret, getSecret, patchSecret, replaceSecretValue } from '../../api/secrets';
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
  problems: [] as { severity: string; pointer?: string; message: string }[],
  yaml: '',
  puts: [] as string[],
  secrets: { create: undefined as unknown, existing: { scope: { all_factories: false, factories: [] as string[] } } },
}));

const keyed = "apiVersion: goal/v1\nspec:\n  runtime:\n    modelKey: '{{model-key}}'\n";
const unkeyed = 'apiVersion: goal/v1\nspec:\n  runtime: {}\n';

vi.mock(import('../../api/definitions'), async (original) => ({
  ...(await original()),
  getVersionModel: vi.fn(async () => ({ model: api.model, ranges: {} }) as never),
  getVersion: vi.fn(async () => ({ files: [{ path: 'factory.yaml', sha256: 's1', size: 10 }] }) as never),
  getVersionFile: vi.fn(async () => ({ text: api.yaml, sha256: 's1' }) as never),
  openDraft: vi.fn(async () => ({ revision: 1 }) as never),
  getFactory: vi.fn(async () => api.factory as Factory),
  validateDraft: vi.fn(
    async () => ({ ok: true, revision: 1, counts: {}, checks: [], problems: api.problems }) as never,
  ),
  getDraft: vi.fn(async () => ({ base: null, revision: 1 }) as never),
  listDraftFiles: vi.fn(async () => [{ path: 'factory.yaml', sha256: 's1', size: 10 }] as never),
  getDraftFile: vi.fn(async () => ({ text: api.yaml, sha256: 's1' })),
  putDraftFile: vi.fn(async (_o: string, _f: string, _p: string, text: string) => {
    api.puts.push(text);
    return {
      etag: 'n',
      revision: 3,
      updated_at: '2026-10-28T10:00:00Z',
      sections: [],
      change_count: 1,
      problem_count: 0,
    };
  }),
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

vi.mock(import('../../api/secrets'), async (original) => ({
  ...(await original()),
  createSecret: vi.fn(async () => {
    if (api.secrets.create) throw api.secrets.create;
    return {} as never;
  }),
  getSecret: vi.fn(async () => api.secrets.existing as never),
  patchSecret: vi.fn(async () => ({}) as never),
  replaceSecretValue: vi.fn(async () => ({}) as never),
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
  // jsdom has no scrollIntoView; the line editor scrolls a focused control into view.
  Element.prototype.scrollIntoView = vi.fn();
  api.saves = [];
  api.puts = [];
  api.yaml = keyed;
  api.secrets.create = undefined;
  api.secrets.existing = { scope: { all_factories: false, factories: [] } };
  api.problems = [];
  setFactory();
  setDraft();
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

/** A published factory with no draft: version 7 runs. */
function setPublished() {
  api.factory = {
    id: factoryId,
    name: 'software',
    draft: null,
    current: { id: 'version-7', number: 7 },
  } as unknown as Factory;
}

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
    expect(screen.getByRole('region', { name: 'Stations' }).getAttribute('tabindex')).toBe('0');
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

  it('says "No limit" in the WIP field of a station with no wip, and + sets it to 1', async () => {
    setDraft({
      line: {
        stations: [
          { id: 'work', name: 'Work', kind: 'agent', agent: 'worker' },
          { id: 'done', name: 'Done', kind: 'done' },
        ],
      },
    });
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    const work = await screen.findByRole('region', { name: 'Work' });
    const field = within(work).getByRole('textbox', { name: 'WIP limit of Work' }) as HTMLInputElement;
    expect(field.placeholder).toBe('No limit');
    expect(field.value).toBe('');
    fireEvent.click(within(work).getByRole('button', { name: 'Raise the WIP limit of Work' }));
    await waitFor(() => expect(field.value).toBe('1'));
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
    const router = renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    const next = await screen.findByRole('button', { name: 'Continue' });
    await waitFor(() => expect((next as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(next);
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/budget'));
    expect(router.state.location.search).toMatchObject({ factory: factoryId });
  });
});

const names = () =>
  within(screen.getByRole('list', { name: 'The stations' }))
    .getAllByRole('region')
    .map((r) => r.getAttribute('aria-label'));

describe('shaping the line', () => {
  const blank = () =>
    setDraft({
      line: {
        stations: [
          { id: 'work', name: 'Work', kind: 'agent', agent: 'worker', wip: 1 },
          { id: 'done', name: 'Done', kind: 'done' },
        ],
      },
      agents: { agents: [{ name: 'worker' }] },
    });

  it('renders the Work card of a blank draft and lets Continue through', async () => {
    blank();
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    expect(await screen.findByRole('region', { name: 'Work' })).toBeTruthy();
    const next = screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement;
    await waitFor(() => expect(next.disabled).toBe(false));
    expect(screen.queryByText(/then continue/)).toBeNull();
  });

  it('adds a station at the control that was pressed', async () => {
    blank();
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    await screen.findByRole('region', { name: 'Work' });
    const adds = screen.getAllByRole('button', { name: 'Add a station here' });
    expect(adds).toHaveLength(2);
    fireEvent.click(adds[1] as HTMLElement);
    await waitFor(() => expect(names()).toEqual(['Work', 'New station', 'Done']));
    fireEvent.click(screen.getAllByRole('button', { name: 'Add a station here' })[0] as HTMLElement);
    await waitFor(() => expect(names()).toEqual(['New station', 'Work', 'New station', 'Done']));
  });

  it('moves and removes a station, and announces each move', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    await screen.findByRole('region', { name: 'Spec' });
    fireEvent.click(screen.getByRole('button', { name: 'Move Spec later' }));
    await waitFor(() => expect(names()).toEqual(['Intake', 'Build', 'Spec', 'Review', 'Done']));
    expect(screen.getByRole('status', { name: '' }).textContent).toBe('Spec moved to position 3 of 5');
    fireEvent.click(screen.getByRole('button', { name: 'Move Spec earlier' }));
    await waitFor(() => expect(names()).toEqual(['Intake', 'Spec', 'Build', 'Review', 'Done']));
    expect(screen.getByText('Spec moved to position 2 of 5')).toBeTruthy();

    expect(within(screen.getByRole('region', { name: 'Done' })).queryByRole('button')).toBeNull();
    expect(screen.getByRole('button', { name: 'Move Intake earlier' }).getAttribute('aria-disabled')).toBe('true');

    fireEvent.click(screen.getByRole('button', { name: 'Remove Build' }));
    await waitFor(() => expect(names()).toEqual(['Intake', 'Spec', 'Review', 'Done']));
  });

  it('holds Continue with a sentence while no station runs an agent, and frees it once one is added', async () => {
    setDraft({
      line: {
        stations: [
          { id: 'inbox', name: 'Inbox', kind: 'queue' },
          { id: 'done', name: 'Done', kind: 'done' },
        ],
      },
    });
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    const next = (await screen.findByRole('button', { name: 'Continue' })) as HTMLButtonElement;
    const sentence = 'Add a station that runs an agent, then continue.';
    expect(await screen.findByText(sentence)).toBeTruthy();
    expect(next.disabled).toBe(true);
    expect(sentence).not.toContain('/');
    fireEvent.click(screen.getAllByRole('button', { name: 'Add a station here' })[1] as HTMLElement);
    await waitFor(() => expect(screen.queryByText(sentence)).toBeNull());
    await waitFor(() => expect(next.disabled).toBe(false));
  });

  it('holds Continue with a sentence while validation reports an error', async () => {
    api.problems = [{ severity: 'error', pointer: '/line/stations/0/agent', message: 'agent is missing' }];
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    const sentence = 'Fix the problems in this line, then continue.';
    expect(await screen.findByText(sentence)).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByText(/\/line/)).toBeNull();

    api.problems = [];
    const spec = screen.getByRole('region', { name: 'Spec' });
    fireEvent.change(within(spec).getByRole('combobox', { name: 'Agent of Spec' }), { target: { value: 'reviewer' } });
    await waitFor(() => expect(screen.queryByText(sentence)).toBeNull(), { timeout: 3000 });
    await waitFor(() =>
      expect((screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement).disabled).toBe(false),
    );
  });
});

describe('the model key', () => {
  const missing = 'Add the model key, then continue.';
  const typeKey = (value: string) =>
    fireEvent.change(screen.getByLabelText('Model key', { selector: 'input' }), { target: { value } });

  it('holds Continue without a key, saves one open to this factory and writes the reference', async () => {
    api.yaml = unkeyed;
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    expect(await screen.findByText(missing)).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement).disabled).toBe(true);

    typeKey('sk-ant-oat-test');
    fireEvent.click(screen.getByRole('button', { name: 'Save the key' }));
    await waitFor(() => expect(screen.queryByText(missing)).toBeNull());
    expect(createSecret).toHaveBeenCalledWith(
      'org-1',
      expect.objectContaining({
        name: 'model-key',
        value: 'sk-ant-oat-test',
        scope: { all_factories: false, factories: [factoryId] },
      }),
    );
    expect(await screen.findByText('Saved as the secret model-key, open to this factory.')).toBeTruthy();
    await waitFor(() =>
      expect((screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement).disabled).toBe(false),
    );
    await waitFor(() => expect(api.puts.at(-1)).toContain('{{model-key}}'), { timeout: 4000 });
    expect(screen.queryByText(/\{\{/)).toBeNull();
  });

  it('saves the model key before Continue leaves the step', async () => {
    api.yaml = unkeyed;
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    await screen.findByText(missing);
    typeKey('sk-ant-oat-test');
    fireEvent.click(screen.getByRole('button', { name: 'Save the key' }));
    await screen.findByText('Saved as the secret model-key, open to this factory.');
    const next = (await screen.findByRole('button', { name: 'Continue' })) as HTMLButtonElement;
    await waitFor(() => expect(next.disabled).toBe(false));
    fireEvent.click(next);
    await waitFor(() => expect(api.puts.at(-1)).toContain('{{model-key}}'), { timeout: 1500 });
  });

  it('asks for a key before saving', async () => {
    api.yaml = unkeyed;
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    await screen.findByText(missing);
    fireEvent.click(screen.getByRole('button', { name: 'Save the key' }));
    expect(await screen.findByText('Paste the model key.')).toBeTruthy();
    expect(createSecret).not.toHaveBeenCalled();
  });

  it('offers the existing secret when the name is taken, and never writes its value', async () => {
    api.yaml = unkeyed;
    api.secrets.create = new ApiError({ title: 'Conflict', status: 409, code: 'secret-name-taken', detail: 'taken' });
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    await screen.findByText(missing);
    typeKey('sk-ant-api-test');
    fireEvent.click(screen.getByRole('button', { name: 'Save the key' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Use the existing secret' }));
    await waitFor(() => expect(screen.queryByText(missing)).toBeNull());
    expect(patchSecret).toHaveBeenCalledWith('org-1', 'model-key', {
      scope: { all_factories: false, factories: [factoryId] },
    });
    expect(getSecret).toHaveBeenCalled();
    expect(replaceSecretValue).not.toHaveBeenCalled();
    expect(createSecret).toHaveBeenCalledTimes(1);
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

describe('step 4 on a published factory with no draft', () => {
  beforeEach(() => setPublished());

  it('shows the running version’s stations, with no message about choosing a template', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    expect(await screen.findByRole('region', { name: 'Spec' })).toBeTruthy();
    expect(screen.queryByText(/Choose a template/)).toBeNull();
    expect(screen.queryByText(/has no draft/)).toBeNull();
    expect(openDraft).not.toHaveBeenCalled();
  });

  it('opens a draft from the version on the first change, then saves it', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    const spec = await screen.findByRole('region', { name: 'Spec' });
    fireEvent.change(within(spec).getByRole('combobox', { name: 'Agent of Spec' }), { target: { value: 'reviewer' } });
    await waitFor(() => expect(api.saves.at(-1)?.section).toBe('line'), { timeout: 3000 });
    expect(openDraft).toHaveBeenCalledTimes(1);
    expect(vi.mocked(openDraft).mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(saveDraftSection).mock.invocationCallOrder[0] ?? 0,
    );
  });

  it('lets Continue go to Budget when nothing changed', async () => {
    renderStep(`/o/acme/get-started/line?factory=${factoryId}`);
    await screen.findByRole('region', { name: 'Spec' });
    await waitFor(
      () => expect((screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement).disabled).toBe(false),
      { timeout: 3000 },
    );
  });
});
