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
import {
  getDraftFile,
  getFactory,
  getVersionModel,
  openDraft,
  publishDraft,
  putDraftFile,
} from '../../api/definitions';
import { getIntake } from '../../api/intake';
import type { Factory, Intake, IntakeTrigger, OrgSummary } from '../../api/types';
import { OrgContext } from '../../shared/org';
import { TooltipProvider } from '../../ui/tooltip';
import { onboardingSearch } from './code-search';
import { IntakeStep } from './intake-step';

vi.mock('../../api/intake', () => ({ getIntake: vi.fn() }));
vi.mock('../../api/definitions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/definitions')>()),
  getFactory: vi.fn(),
  getVersionModel: vi.fn(),
  openDraft: vi.fn(),
  getDraftFile: vi.fn(),
  putDraftFile: vi.fn(),
  publishDraft: vi.fn(),
}));

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factoryId = '6f1d2c3a-4b5e-4f70-8a9b-0c1d2e3f4a5b';

const yaml = `# The factory. Keep this comment.
spec:
  board:
    triggers:
      - name: issues-labeled-factory
        enabled: false
        on: github.issues.labeled
        create: { type: feature, station: spec }
      - name: flaky-test-on-main
        enabled: false
        on: github.check_run.failed
        create: { type: fix, station: build, expedite: true }
      - name: weekly-dependency-audit
        enabled: false
        on: schedule
        create: { type: infra, station: spec }
      - name: nightly-review
        enabled: false
        on: schedule
        create: { type: infra, station: spec }
`;

/** A starter trigger as getIntake shows it; the template's are off and need no endpoint only when told so. */
function trigger(over: Partial<IntakeTrigger> & Pick<IntakeTrigger, 'name'>): IntakeTrigger {
  return {
    enabled: false,
    source: 'schedule',
    on: 'schedule',
    when: '0 9 * * MON, UTC',
    create: { type: 'infra', station: 'spec', lane: null, expedite: false },
    days: [0, 0, 0, 0, 0, 0, 0],
    problem: null,
    ...over,
  };
}

const issues = trigger({
  name: 'issues-labeled-factory',
  source: 'github',
  on: 'github.issues.labeled',
  when: 'github.issues.labeled',
  create: { type: 'feature', station: 'spec', lane: null, expedite: false },
  problem: { kind: 'missing_connection', message: 'No GitHub endpoint for this trigger.' },
});
const flaky = trigger({
  name: 'flaky-test-on-main',
  source: 'github',
  on: 'github.check_run.failed',
  when: 'github.check_run.failed',
  create: { type: 'fix', station: 'build', lane: null, expedite: true },
  problem: { kind: 'missing_connection', message: 'No GitHub endpoint for this trigger.' },
});
const audit = trigger({ name: 'weekly-dependency-audit', when: '0 9 * * MON, UTC' });

/** The starter triggers of the template: two need a GitHub endpoint the factory does not have yet. */
const starter: IntakeTrigger[] = [issues, flaky, audit];

function intakeOf(triggers: IntakeTrigger[]): Intake {
  return {
    items: 0,
    from: null,
    to: null,
    sources: { github: 0, people: 0, cli: 0, api: 0, schedule: 0, webhook: 0 },
    triggers,
  };
}

function factory(over: Partial<Factory> = {}): Factory {
  return {
    id: factoryId,
    name: 'code-review',
    title: 'Code review',
    current: { id: 'version-1', number: 1 },
    draft: null,
    ...over,
  } as unknown as Factory;
}

beforeEach(() => {
  vi.mocked(getFactory).mockResolvedValue(factory());
  vi.mocked(getVersionModel).mockResolvedValue({
    model: { basics: { name: 'code-review', purpose: 'Reviews pull requests.' }, line: { stations: [] }, review: {} },
  } as never);
  vi.mocked(getIntake).mockResolvedValue(intakeOf(starter));
  vi.mocked(openDraft).mockResolvedValue({ revision: 1, change_count: 0 } as never);
  vi.mocked(getDraftFile).mockResolvedValue({ text: yaml, sha256: 'sha-1' } as never);
  vi.mocked(putDraftFile).mockResolvedValue({ revision: 2 } as never);
  vi.mocked(publishDraft).mockResolvedValue({ id: 'version-2' } as never);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

/** Step 6 at /o/acme/get-started/intake with the URL's `search`; the other steps are stubs the links reach. */
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
      if (key === 'intake') return <IntakeStep org={org} search={search} />;
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

const url = `/o/acme/get-started/intake?factory=${factoryId}`;

/** The row of a trigger, by the name the switch is labelled with. */
function rowOf(name: string): HTMLElement {
  const row = screen.getByRole('switch', { name }).closest('div.border-t');
  if (!row) throw new Error(`no row for ${name}`);
  return row as HTMLElement;
}

describe('step 6, choose where work comes from', () => {
  it('sends the person back to step 3 when the URL has no factory', async () => {
    renderStep('/o/acme/get-started/intake');
    expect(await screen.findByText('Choose a template first, then pick where work comes from.')).toBeTruthy();
    const back = screen.getByRole('link', { name: 'Back' });
    expect(back.getAttribute('href')).toContain('/o/acme/get-started/factory');
  });

  it('shows loading while the triggers are read', async () => {
    vi.mocked(getIntake).mockReturnValue(new Promise(() => {}));
    renderStep(url);
    expect((await screen.findByRole('status')).textContent).toBe('Loading');
  });

  it('shows the error with Retry, and Retry reads the triggers again', async () => {
    vi.mocked(getIntake).mockRejectedValueOnce(new Error('network'));
    renderStep(url);
    expect(await screen.findByText('The factory’s triggers can’t be read.')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByRole('switch', { name: 'weekly-dependency-audit' })).toBeTruthy();
    expect(getIntake).toHaveBeenCalledTimes(2);
  });

  it('says there are no starter triggers and continues without a write', async () => {
    vi.mocked(getIntake).mockResolvedValue(intakeOf([]));
    renderStep(url);
    expect(await screen.findByText(/This factory has no starter triggers/)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Starter triggers' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    expect(await screen.findByText('Step people')).toBeTruthy();
    expect(openDraft).not.toHaveBeenCalled();
  });

  it('lists each trigger with what it creates, and its switch starts at its enabled value', async () => {
    vi.mocked(getIntake).mockResolvedValue(
      intakeOf([issues, trigger({ name: 'weekly-dependency-audit', enabled: true, when: '0 9 * * MON, UTC' })]),
    );
    renderStep(url);
    expect(await screen.findByRole('heading', { name: 'Starter triggers' })).toBeTruthy();

    expect(within(rowOf('issues-labeled-factory')).getByText('Creates a feature at spec')).toBeTruthy();
    expect(within(rowOf('weekly-dependency-audit')).getByText('0 9 * * MON, UTC')).toBeTruthy();
    expect(screen.getByRole('switch', { name: 'weekly-dependency-audit' }).getAttribute('aria-checked')).toBe('true');
    expect(screen.getByRole('switch', { name: 'weekly-dependency-audit' }).getAttribute('aria-disabled')).toBe('true');
    expect(screen.getByText('Always on:')).toBeTruthy();
    expect(screen.getByText('New item')).toBeTruthy();
    expect(screen.getByText('goal item create')).toBeTruthy();
    expect(screen.getByText('The API')).toBeTruthy();
  });

  it('says an expedited trigger creates its item in the expedite lane', async () => {
    renderStep(url);
    expect(await screen.findByText('Creates a fix at build in the expedite lane')).toBeTruthy();
  });

  it('shows Connect for a trigger that needs an endpoint, with its switch off and disabled', async () => {
    renderStep(url);
    // Both GitHub triggers need the endpoint, so each one has its own Connect link.
    const connects = await screen.findAllByRole('link', { name: 'Connect GitHub' });
    expect(connects).toHaveLength(2);
    expect(connects[0]?.getAttribute('href')).toBe('/o/acme/f/code-review/intake/webhooks');

    const sw = screen.getByRole('switch', { name: 'issues-labeled-factory' });
    expect(sw.getAttribute('aria-checked')).toBe('false');
    expect(sw.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(sw);
    expect(sw.getAttribute('aria-checked')).toBe('false');
  });

  it('counts only the switched triggers that were off, and says so on the button', async () => {
    renderStep(url);
    const button = await screen.findByRole('button', { name: 'Continue' });
    expect(button).toBeTruthy();

    fireEvent.click(screen.getByRole('switch', { name: 'weekly-dependency-audit' }));
    expect(screen.getByRole('button', { name: 'Turn on 1 trigger' })).toBeTruthy();
  });

  it('turns on every chosen trigger with one draft, one save and one publish, then goes to step 7', async () => {
    vi.mocked(getIntake).mockResolvedValue(
      intakeOf([
        ...starter,
        trigger({ name: 'deploy-failed', source: 'webhook', on: 'webhook.cloudflare.deploy.failed' }),
      ]),
    );
    renderStep(url);
    await screen.findByRole('switch', { name: 'weekly-dependency-audit' });
    fireEvent.click(screen.getByRole('switch', { name: 'weekly-dependency-audit' }));
    expect(screen.getByRole('button', { name: 'Turn on 1 trigger' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Turn on 1 trigger' }));
    expect(await screen.findByText('Step people')).toBeTruthy();

    expect(openDraft).toHaveBeenCalledTimes(1);
    expect(putDraftFile).toHaveBeenCalledTimes(1);
    const written = vi.mocked(putDraftFile).mock.calls[0]?.[3] as string;
    expect(written.startsWith('# The factory. Keep this comment.')).toBe(true);
    expect(written).toMatch(/name: weekly-dependency-audit\n\s+enabled: true/);
    expect(written).toMatch(/name: issues-labeled-factory\n\s+enabled: false/);
    expect(publishDraft).toHaveBeenCalledTimes(1);
    expect(publishDraft).toHaveBeenCalledWith(
      org.id,
      factoryId,
      expect.objectContaining({ expected_current_version_id: 'version-1' }),
    );
  });

  it('turns on several triggers in one publish', async () => {
    vi.mocked(getIntake).mockResolvedValue(
      intakeOf([
        trigger({ name: 'nightly-review', when: '0 2 * * *, UTC' }),
        trigger({ name: 'weekly-dependency-audit' }),
      ]),
    );
    renderStep(url);
    fireEvent.click(await screen.findByRole('switch', { name: 'nightly-review' }));
    fireEvent.click(screen.getByRole('switch', { name: 'weekly-dependency-audit' }));
    expect(screen.getByRole('button', { name: 'Turn on 2 triggers' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Turn on 2 triggers' }));
    expect(await screen.findByText('Step people')).toBeTruthy();
    expect(putDraftFile).toHaveBeenCalledTimes(1);
    expect(publishDraft).toHaveBeenCalledTimes(1);
    const written = vi.mocked(putDraftFile).mock.calls[0]?.[3] as string;
    expect(written).toMatch(/name: weekly-dependency-audit\n\s+enabled: true/);
  });

  it('shows a refused publish on the step and stays on it', async () => {
    vi.mocked(getIntake).mockResolvedValue(intakeOf([trigger({ name: 'weekly-dependency-audit' })]));
    vi.mocked(publishDraft).mockRejectedValue(
      new ApiError({
        type: 'about:blank',
        title: 'Version conflict',
        status: 412,
        detail: 'Another version was published.',
        code: 'draft-base-moved',
      } as never),
    );
    renderStep(url);
    fireEvent.click(await screen.findByRole('switch', { name: 'weekly-dependency-audit' }));
    fireEvent.click(screen.getByRole('button', { name: 'Turn on 1 trigger' }));

    expect(
      await screen.findByText('Another version was published while you were here. Reload the step to see it.'),
    ).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Choose where work comes from' })).toBeTruthy();
  });

  it('says a draft that is already open holds the change, and writes nothing', async () => {
    vi.mocked(getIntake).mockResolvedValue(intakeOf([trigger({ name: 'weekly-dependency-audit' })]));
    vi.mocked(openDraft).mockResolvedValue({ revision: 3, change_count: 2 } as never);
    renderStep(url);
    fireEvent.click(await screen.findByRole('switch', { name: 'weekly-dependency-audit' }));
    fireEvent.click(screen.getByRole('button', { name: 'Turn on 1 trigger' }));

    expect(await screen.findByText(/The factory has an open draft/)).toBeTruthy();
    expect(putDraftFile).not.toHaveBeenCalled();
    expect(publishDraft).not.toHaveBeenCalled();
  });

  it('reads the panel’s Intake card from the switches, as they are switched', async () => {
    vi.mocked(getIntake).mockResolvedValue(intakeOf([trigger({ name: 'weekly-dependency-audit' }), issues]));
    renderStep(url);
    const card = await screen.findByRole('region', { name: 'Intake' });
    expect(within(card).queryByText('weekly-dependency-audit')).toBeNull();

    fireEvent.click(screen.getByRole('switch', { name: 'weekly-dependency-audit' }));
    await waitFor(() => expect(within(card).getByText('weekly-dependency-audit')).toBeTruthy());
    expect(within(card).getByText('And New item, CLI, API')).toBeTruthy();
  });
});
