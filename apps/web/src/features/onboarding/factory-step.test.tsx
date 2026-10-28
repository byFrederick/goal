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
import type { Factory, NewFactoryRequest, OrgSummary, SectionSave, Template, TemplateSummary } from '../../api/types';
import { factoryModelOf, type Section } from '../../shared/definition/model';
import { OrgContext } from '../../shared/org';
import { TooltipProvider } from '../../ui/tooltip';
import { onboardingSearch } from './code-search';
import { FactoryStep } from './factory-step';
import { LineStep } from './line-step';

// Goald answers the catalog, the create call and the new factory's draft; the step reads them through the API
// modules. `api` is what a test sets: the templates, a hold or a refusal of the next create, and what was sent.
const api = vi.hoisted(() => ({
  templates: [] as unknown[],
  blank: undefined as unknown,
  hold: false,
  templatesFail: 0,
  createRefusal: null as unknown,
  created: [] as unknown[],
  saves: [] as { section: Section; value: unknown }[],
}));

vi.mock(import('../../api/definitions'), async (original) => ({
  ...(await original()),
  listTemplates: vi.fn(async () => {
    if (api.hold) return new Promise<never>(() => {});
    if (api.templatesFail > 0) {
      api.templatesFail -= 1;
      throw new Error('goald is not answering');
    }
    return [...api.templates, ...(api.blank ? [api.blank] : [])] as TemplateSummary[];
  }),
  getTemplate: vi.fn(async (_org: string, name: string) => {
    const t = [...api.templates, ...(api.blank ? [api.blank] : [])].find((x) => (x as { name: string }).name === name);
    return t as Template;
  }),
  createFactory: vi.fn(async (_org: string, request: NewFactoryRequest) => {
    if (api.createRefusal) throw api.createRefusal;
    api.created.push(request);
    return { id: factoryId, name: request.name, draft: { revision: 1 }, current: null } as unknown as Factory;
  }),
  getFactory: vi.fn(
    async (_org: string, id: string) =>
      ({ id, name: 'software', draft: { revision: 1 }, current: null }) as unknown as Factory,
  ),
  getDraft: vi.fn(async () => ({ base: null, revision: 1 }) as never),
  getDraftModel: vi.fn(
    async () =>
      ({
        model: factoryModelOf({
          basics: { name: 'software' },
          line: { stations: [] },
          agents: { agents: [] },
          connections: [],
          review: {},
        }),
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

/** The id goald gives the factory every create call makes in these tests. */
const factoryId = '6f1d2c3a-4b5e-4f70-8a9b-0c1d2e3f4a5b';

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 0, role: 'owner' };

const software = {
  name: 'software',
  version: 1,
  title: 'Software delivery',
  summary: 'Requests become small pull requests a reviewer merges.',
  stations: 3,
  agents: 2,
  connectors: [],
  changed: '2026-10-01',
} as unknown as TemplateSummary;

const incident = {
  name: 'incident-response',
  version: 1,
  title: 'Incident response',
  summary: 'Alerts become investigations, fixes and postmortems.',
  stations: 4,
  agents: 3,
  connectors: [],
  changed: '2026-10-02',
} as unknown as TemplateSummary;

/** The line of each template as getTemplate answers it: the stations the card chips show. */
function lineOf(name: string, stations: { id: string; name: string; kind: string; wip?: number }[]) {
  return {
    name,
    model: factoryModelOf({
      basics: {},
      line: { stations },
      agents: { agents: [] },
      connections: [],
      review: {},
    } as never),
  };
}

beforeEach(() => {
  api.templates = [
    {
      ...software,
      model: lineOf('software', [
        { id: 'spec', name: 'Spec', kind: 'agent', wip: 6 },
        { id: 'build', name: 'Build', kind: 'agent', wip: 6 },
        { id: 'review', name: 'Review', kind: 'agent', wip: 3 },
      ]).model,
    },
    {
      ...incident,
      model: lineOf('incident-response', [
        { id: 'triage', name: 'Triage', kind: 'agent' },
        { id: 'investigate', name: 'Investigate', kind: 'agent' },
      ]).model,
    },
  ];
  api.blank = undefined;
  api.hold = false;
  api.templatesFail = 0;
  api.createRefusal = null;
  api.created = [];
  api.saves = [];
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

/** Step 3 at /o/acme/get-started/factory with the URL's `search`; the other steps are stubs the links reach. */
function renderStep(url = '/o/acme/get-started/factory?connection=github&owner=acme&repos=acme%2Fgoal') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: Outlet });
  const step = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/get-started/$step',
    validateSearch: (search: Record<string, unknown>) => onboardingSearch(search),
    component: function Page() {
      const search = step.useSearch();
      const { step: key } = step.useParams();
      if (key === 'factory') return <FactoryStep org={org} search={search} />;
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

describe('step 3, pick a template', () => {
  it('shows the loading state while the templates load', async () => {
    api.hold = true;
    renderStep();
    expect((await screen.findByRole('status')).textContent).toBe('Loading');
  });

  it('shows the error with Retry, and loads the templates again on Retry', async () => {
    api.templatesFail = 1;
    renderStep();
    expect(await screen.findByRole('alert')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByRole('radio', { name: /Software delivery/ })).toBeTruthy();
  });

  it('lists the templates as cards with their line, and chooses the first by default', async () => {
    renderStep();
    // The templates are listed by title, so Incident response is first.
    const incidentCard = await screen.findByRole('radio', { name: /Incident response/ });
    expect((incidentCard as HTMLInputElement).checked).toBe(true);
    const card = incidentCard.closest('label') as HTMLElement;
    expect(within(card).getByText('Triage')).toBeTruthy();
    expect(within(card).getByText('Investigate')).toBeTruthy();
    const software = screen.getByRole('radio', { name: /Software delivery/ });
    expect(within(software.closest('label') as HTMLElement).getByText('Review')).toBeTruthy();
    expect(screen.getByRole('radio', { name: /Start empty/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Use Incident response' })).toBeTruthy();
  });

  it('shows the factory name with its initial, and the colour as not available yet', async () => {
    renderStep();
    await screen.findByRole('radio', { name: /Incident response/ });
    const name = screen.getByRole('textbox', { name: 'Factory name' });
    expect((name as HTMLInputElement).value).toBe('incident-response');
    expect(screen.getByRole('button', { name: 'Not available yet' }).getAttribute('aria-disabled')).toBe('true');
  });

  it('creates the factory from the chosen template and goes to step 4 with its id', async () => {
    const router = renderStep();
    await screen.findByRole('radio', { name: /Incident response/ });
    fireEvent.click(screen.getByRole('radio', { name: /Incident response/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Use Incident response' }));
    await waitFor(() => expect(api.created).toHaveLength(1));
    expect(api.created[0]).toEqual({
      name: 'incident-response',
      start: { kind: 'template', template: 'incident-response' },
    });
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/line'), { timeout: 4000 });
    expect(router.state.location.search).toMatchObject({ connection: 'github', owner: 'acme', factory: factoryId });
  });

  it('saves the chosen repository and the purpose into the new draft before step 4', async () => {
    const router = renderStep();
    await screen.findByRole('radio', { name: /Incident response/ });
    fireEvent.change(screen.getByRole('textbox', { name: 'What it does' }), { target: { value: 'Ships the app.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Use Incident response' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/get-started/line'), { timeout: 4000 });
    const basics = api.saves.filter((s) => s.section === 'basics').at(-1)?.value as Record<string, unknown>;
    expect(basics.repository).toEqual({ url: 'https://github.com/acme/goal', connection: 'github' });
    expect(basics.purpose).toBe('Ships the app.');
    expect(api.created).toHaveLength(1);
  });

  it('says the name is taken on the name field, which takes focus', async () => {
    api.createRefusal = new ApiError({
      title: 'Name taken',
      status: 409,
      code: 'name-taken',
      detail: 'taken',
    } as ConstructorParameters<typeof ApiError>[0]);
    renderStep();
    await screen.findByRole('radio', { name: /Incident response/ });
    const name = screen.getByRole('textbox', { name: 'Factory name' });
    fireEvent.click(screen.getByRole('button', { name: 'Use Incident response' }));
    expect(
      await screen.findByText('Acme already has a factory named incident-response. Choose another name.'),
    ).toBeTruthy();
    await waitFor(() => expect(document.activeElement).toBe(name));
  });

  it('reuses the factory when Back from step 4 brings the person here again', async () => {
    renderStep(`/o/acme/get-started/factory?factory=${factoryId}`);
    expect(await screen.findByRole('link', { name: 'Continue' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Use / })).toBeNull();
  });
});
