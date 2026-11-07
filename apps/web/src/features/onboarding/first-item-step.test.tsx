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
import { getItem, listInvitations, listItemEvents, listItems, listMembers, submitItem } from '../../api/client';
import {
  getDraft,
  getDraftFile,
  getDraftModel,
  getFactory,
  getVersion,
  getVersionFile,
  getVersionModel,
  listDraftFiles,
  openDraft,
  publishDraft,
  putDraftFile,
  validateDraft,
} from '../../api/definitions';
import { getIntake } from '../../api/intake';
import { createSecret } from '../../api/secrets';
import type { Factory, FactoryModel, Intake, Item, ItemEvent, OrgSummary } from '../../api/types';
import { OrgContext } from '../../shared/org';
import { TooltipProvider } from '../../ui/tooltip';
import { onboardingSearch } from './code-search';
import { FirstItemStep } from './first-item-step';

vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  getItem: vi.fn(),
  listItems: vi.fn(),
  listItemEvents: vi.fn(),
  submitItem: vi.fn(),
  listMembers: vi.fn(),
  listInvitations: vi.fn(),
}));
vi.mock('../../api/definitions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/definitions')>()),
  getFactory: vi.fn(),
  getVersion: vi.fn(),
  getVersionFile: vi.fn(),
  getVersionModel: vi.fn(),
  getDraft: vi.fn(),
  getDraftModel: vi.fn(),
  listDraftFiles: vi.fn(),
  getDraftFile: vi.fn(),
  putDraftFile: vi.fn(),
  validateDraft: vi.fn(),
  publishDraft: vi.fn(),
  openDraft: vi.fn(),
}));
vi.mock('../../api/secrets', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/secrets')>()),
  createSecret: vi.fn(),
}));
vi.mock('../../api/intake', () => ({ getIntake: vi.fn() }));

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factoryId = '6f1d2c3a-4b5e-4f70-8a9b-0c1d2e3f4a5b';
const title = 'Add a contributing guide to the README';

const starter = {
  title,
  type: 'feature',
  body: 'Add a short contributing guide to README.md, one file and two criteria.',
};

/** The model as the published version has it: the template's stations and its starter item. */
function modelWith(item: typeof starter | undefined): FactoryModel {
  return {
    basics: { name: 'software-delivery', purpose: 'Turns requests into small, reviewed pull requests.' },
    line: {
      stations: [
        { id: 'inbox', name: 'Inbox', kind: 'queue' },
        { id: 'spec', name: 'Spec', kind: 'agent' },
        { id: 'build', name: 'Build', kind: 'agent' },
        { id: 'review', name: 'Review', kind: 'human' },
        { id: 'done', name: 'Done', kind: 'done' },
      ],
    },
    review: {},
    ...(item ? { starter: { item } } : {}),
  } as unknown as FactoryModel;
}

const created = Date.now() - 9 * 60_000;

/** The starter item nine minutes in, at Build, with $0.84 spent (the mock's picture). */
function itemOf(over: Partial<Item> = {}): Item {
  return {
    id: 'item-1',
    ref: 'software-delivery#1',
    factory_id: factoryId,
    number: 1,
    type: 'feature',
    title,
    body: starter.body,
    station: 'build',
    state: 'in_progress',
    class_of_service: 'standard',
    priority: 0,
    rework_count: 0,
    cost_usd: 0.84,
    outputs: {},
    created_at: new Date(created).toISOString(),
    updated_at: new Date(created + 302_000).toISOString(),
    ...over,
  } as Item;
}

const events: ItemEvent[] = [
  {
    id: 1,
    item_id: 'item-1',
    at: new Date(created).toISOString(),
    kind: 'created',
    station: 'inbox',
    actor: 'engine',
    data: {},
  },
  {
    id: 2,
    item_id: 'item-1',
    at: new Date(created + 12_000).toISOString(),
    kind: 'moved',
    station: 'spec',
    actor: 'engine',
    data: { from: 'inbox', to: 'spec' },
  },
  {
    id: 3,
    item_id: 'item-1',
    at: new Date(created + 270_000).toISOString(),
    kind: 'approved',
    station: 'spec',
    actor: 'user:local',
    data: {},
  },
] as ItemEvent[];

const keyed = "apiVersion: goal/v1\nspec:\n  runtime:\n    modelKey: '{{model-key}}'\n";
const unkeyed = 'apiVersion: goal/v1\nspec:\n  runtime: {}\n';

let items: Item[];
let current: Item;
let published: string;

beforeEach(() => {
  items = [];
  current = itemOf();
  vi.mocked(getFactory).mockResolvedValue({
    id: factoryId,
    name: 'software-delivery',
    title: 'Software delivery',
    current: { id: 'version-1', number: 1 },
    draft: null,
  } as unknown as Factory);
  vi.mocked(getVersionModel).mockResolvedValue({ model: modelWith(starter) } as never);
  published = keyed;
  vi.mocked(getVersion).mockResolvedValue({ files: [{ path: 'factory.yaml', sha256: 'sha-1', size: 10 }] } as never);
  vi.mocked(getVersionFile).mockImplementation(async () => ({ text: published, sha256: 'sha-1' }) as never);
  vi.mocked(getIntake).mockResolvedValue({
    items: 0,
    from: null,
    to: null,
    sources: { github: 0, people: 0, cli: 0, api: 0, schedule: 0, webhook: 0 },
    suggestions: [],
    triggers: [],
  } as Intake);
  vi.mocked(listMembers).mockResolvedValue([]);
  vi.mocked(listInvitations).mockResolvedValue([]);
  // The newest item of the factory: the starter once it has been submitted.
  vi.mocked(listItems).mockImplementation(async () => ({ items: items.slice(0, 1), next_after: null }) as never);
  vi.mocked(getItem).mockImplementation(async () => current);
  vi.mocked(listItemEvents).mockImplementation(async () => ({ events, next_after: null }) as never);
  vi.mocked(submitItem).mockImplementation(async () => {
    items = [current];
    return current;
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

/** Step 8 at /o/acme/get-started/first-item with the URL's search. The queries live in `queryClient`, so a remount reads them. */
function renderStep(queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } }), query = '') {
  const root = createRootRoute({ component: Outlet });
  const step = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/get-started/$step',
    validateSearch: (search: Record<string, unknown>) => onboardingSearch(search),
    component: function Page() {
      const search = step.useSearch();
      return <FirstItemStep org={org} search={search} />;
    },
  });
  const router = createRouter({
    routeTree: root.addChildren([step]),
    history: createMemoryHistory({ initialEntries: [`/o/acme/get-started/first-item?factory=${factoryId}${query}`] }),
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
  return queryClient;
}

describe('step 8, watch your first item', () => {
  it('submits the starter item once, then shows its card with the timeline and the cost', async () => {
    renderStep();

    expect(await screen.findByRole('heading', { level: 2, name: title })).toBeTruthy();
    expect(submitItem).toHaveBeenCalledTimes(1);
    expect(submitItem).toHaveBeenCalledWith('org-1', factoryId, {
      title,
      type: 'feature',
      body: starter.body,
    });
    expect(screen.getByText('At Build, 9 min')).toBeTruthy();
    expect(screen.getByText('Created from the setup')).toBeTruthy();
    expect(screen.getByText('Spec accepted')).toBeTruthy();
    expect(screen.getByText('Cost so far')).toBeTruthy();
    expect(screen.getByText('$0.84')).toBeTruthy();
    expect(screen.queryByText(/Review merges it/)).toBeNull();
    // The only repository text is the Code card's detail; the step itself says nothing of one.
    expect(screen.getAllByText(/repository/)).toHaveLength(1);
    expect(screen.getByText("Items don't touch a repository")).toBeTruthy();
  });

  it('says a missing model key in plain words with a link to Settings, and keeps Blocked words without a code', async () => {
    const blockedAt = (id: number, data: Record<string, unknown>) =>
      ({
        id,
        item_id: 'item-1',
        at: new Date(created + 300_000 + id).toISOString(),
        kind: 'blocked',
        station: 'build',
        actor: 'engine',
        data,
      }) as ItemEvent;
    vi.mocked(listItemEvents).mockImplementation(
      async () =>
        ({
          events: [...events, blockedAt(10, { reason: 'names no runtime.modelKey', code: 'model_key_missing' })],
          next_after: null,
        }) as never,
    );
    renderStep();

    const timeline = await screen.findByRole('list', { name: 'Timeline' });
    expect(timeline.textContent).toContain('Agents need a model key: add one in Settings');
    expect(within(timeline).getByRole('link', { name: 'Settings' }).getAttribute('href')).toMatch(
      /\/f\/software-delivery\/settings#runtime$/,
    );
    expect(timeline.textContent).not.toContain('runtime.modelKey');
  });

  it('says nothing about pull requests when the factory only reads code', async () => {
    renderStep(undefined, '&url=https%3A%2F%2Fgithub.com%2Facme%2Fapp&secret=acme-token');

    expect(await screen.findByRole('heading', { level: 2, name: title })).toBeTruthy();
    expect(screen.queryByText(/A change to/)).toBeNull();
  });

  it('does not submit again after a remount, and the panel shows the running item', async () => {
    const queryClient = renderStep();
    await screen.findByRole('heading', { level: 2, name: title });
    cleanup();

    renderStep(queryClient);

    expect(await screen.findByRole('heading', { level: 2, name: title })).toBeTruthy();
    expect(submitItem).toHaveBeenCalledTimes(1);
    const panel = screen.getByRole('region', { name: 'Your first item' });
    expect(within(panel).getByText('At Build, 9 min in, $0.84 so far')).toBeTruthy();
  });

  it('shows the item that already exists and submits nothing', async () => {
    items = [current];
    renderStep();

    expect(await screen.findByRole('heading', { level: 2, name: title })).toBeTruthy();
    expect(submitItem).not.toHaveBeenCalled();
  });

  it('shows the pull request link only once the item has a pull request output', async () => {
    items = [current];
    const queryClient = renderStep();
    await screen.findByRole('heading', { level: 2, name: title });
    expect(screen.queryByRole('link', { name: /Open pull request/ })).toBeNull();

    // The stream's item event refreshes the item by its key.
    current = itemOf({ outputs: { build: { pull_request: 'https://github.com/byFrederick/goal/pull/1' } } });
    await queryClient.invalidateQueries({ queryKey: ['org', 'org-1', 'item', 'item-1'] });

    expect(await screen.findByRole('link', { name: 'Open pull request #1' })).toBeTruthy();
  });

  it('shows the empty state and submits nothing for a factory without a starter item', async () => {
    vi.mocked(getVersionModel).mockResolvedValue({ model: modelWith(undefined) } as never);
    renderStep();

    expect(await screen.findByText(/This factory has no starter item/)).toBeTruthy();
    expect(submitItem).not.toHaveBeenCalled();
    expect(screen.getByRole('link', { name: 'Go to the Floor' })).toBeTruthy();
  });

  it('shows the error state when the item cannot be submitted', async () => {
    vi.mocked(submitItem).mockRejectedValue(new Error('down'));
    renderStep();

    expect(await screen.findByText('The first item can’t be submitted.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy();
  });

  it('shows loading while the factory is read', async () => {
    vi.mocked(getFactory).mockReturnValue(new Promise(() => {}));
    renderStep();
    expect((await screen.findByRole('status')).textContent).toBe('Loading');
  });

  it('sets no refetch interval on any query: the item refreshes from the stream', async () => {
    items = [current];
    const queryClient = renderStep();
    await screen.findByRole('heading', { level: 2, name: title });

    const intervals = queryClient
      .getQueryCache()
      .getAll()
      .flatMap((q) => q.observers.map((o) => o.options.refetchInterval));
    expect(intervals.length).toBeGreaterThan(0);
    expect(intervals.every((i) => !i)).toBe(true);
  });

  describe('without a model key', () => {
    beforeEach(() => {
      published = unkeyed;
      let draftText = unkeyed;
      vi.mocked(getDraft).mockResolvedValue({ base: null, revision: 1 } as never);
      vi.mocked(getDraftModel).mockResolvedValue({
        model: modelWith(starter),
        etags: { basics: 'b1', line: 'l1', agents: 'a1', connections: 'c1', review: 'r1' },
        revision: 1,
      } as never);
      vi.mocked(listDraftFiles).mockResolvedValue([{ path: 'factory.yaml', sha256: 'd1', size: 10 }] as never);
      vi.mocked(getDraftFile).mockImplementation(async () => ({ text: draftText, sha256: 'd1' }) as never);
      vi.mocked(putDraftFile).mockImplementation((async (_o: string, _f: string, _p: string, text: string) => {
        draftText = text;
        return {
          etag: 'n',
          revision: 2,
          updated_at: '2026-10-28T10:00:00Z',
          sections: [],
          change_count: 1,
          problem_count: 0,
        };
      }) as never);
      vi.mocked(validateDraft).mockResolvedValue({
        ok: true,
        revision: 2,
        counts: {},
        checks: [],
        problems: [],
      } as never);
      vi.mocked(openDraft).mockResolvedValue({ revision: 1 } as never);
      vi.mocked(createSecret).mockResolvedValue({} as never);
    });

    it('holds the starter item until the factory has a model key', async () => {
      renderStep();

      expect(await screen.findByText(/Agents need a model key to run/)).toBeTruthy();
      expect(screen.getByLabelText('Model key', { selector: 'input' })).toBeTruthy();
      expect(screen.getByRole('button', { name: 'Publish and start' })).toBeTruthy();
      expect(document.body.textContent).not.toMatch(/\{\{|spec\.runtime/);
      expect(submitItem).not.toHaveBeenCalled();
    });

    it('publishes the key and then submits the starter item once', async () => {
      let finish: (v: unknown) => void = () => {};
      vi.mocked(publishDraft).mockImplementation(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }) as never,
      );
      renderStep();
      await screen.findByText(/Agents need a model key to run/);

      fireEvent.change(screen.getByLabelText('Model key', { selector: 'input' }), { target: { value: 'sk-test' } });
      fireEvent.click(screen.getByRole('button', { name: 'Save the key' }));
      const publish = await screen.findByRole('button', { name: 'Publish and start' });
      await waitFor(() => expect((publish as HTMLButtonElement).disabled).toBe(false));
      expect(submitItem).not.toHaveBeenCalled();
      fireEvent.click(publish);

      await waitFor(() => expect(publishDraft).toHaveBeenCalledTimes(1));
      expect(vi.mocked(publishDraft).mock.calls[0]?.[2]).toMatchObject({ expected_current_version_id: 'version-1' });
      expect(submitItem).not.toHaveBeenCalled();
      finish({});

      expect(await screen.findByRole('heading', { level: 2, name: title })).toBeTruthy();
      expect(submitItem).toHaveBeenCalledTimes(1);
    });
  });
});
