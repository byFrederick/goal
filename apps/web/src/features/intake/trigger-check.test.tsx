// @vitest-environment jsdom
// The page of a trigger with `check` as a person sees it (intake/trigger-check.jpg): the Check block with its
// agent, tools, brief and what it remembers, the limits, the log of checks (done, failed, empty and a try) and
// Try a check. The API boundary is mocked; the routes and the shell are not.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import * as definitions from '../../api/definitions';
import * as intake from '../../api/intake';
import type { Factory, IntakeTriggerDetail, TriggerCheck } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { TriggerPage } from './trigger-page';

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>();
  return {
    ...actual,
    useParams: () => ({ trigger: 'new-incidents' }),
    useSearch: () => ({}),
    Link: ({ children, params }: { children: ReactNode; params?: { ref?: string } }) => (
      <a href={params?.ref ? `/items/${params.ref}` : '/definition'}>{children}</a>
    ),
  };
});
vi.mock('../../app/top-bar', () => ({
  TopBarSlot: ({ actions }: { actions?: ReactNode }) => <div>{actions}</div>,
}));
vi.mock('../../shared/org', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/org')>()),
  useOrg: () => ({ id: 'org-1', slug: 'acme', name: 'Acme' }),
}));
vi.mock('../../shared/factory', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/factory')>()),
  useFactory: () => factory,
}));
vi.mock('../../api/intake', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/intake')>()),
  getIntakeTrigger: vi.fn(),
  listTriggerChecks: vi.fn(),
  tryTriggerCheck: vi.fn(),
  clearTriggerState: vi.fn(),
}));
vi.mock('../../api/definitions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/definitions')>()),
  getFactory: vi.fn(),
  getDraft: vi.fn(),
  getDraftFile: vi.fn(),
  getVersionFile: vi.fn(),
}));

const factory = {
  id: 'factory-1',
  name: 'sre',
  title: 'SRE',
  current: { id: 'version-2', number: 2 },
  draft: null,
} as unknown as Factory;

const file = `apiVersion: goal/v1
kind: Factory
metadata:
  name: sre
spec:
  agents:
    - { name: watcher, prompt: p, builtins: { read: allow } }
    - { name: scribe, prompt: p }
  board:
    workTypes: [incident]
    stations:
      - { id: triage, kind: agent }
    triggers:
      - name: new-incidents
        on: schedule
        cron: "*/5 * * * *"
        check: { agent: watcher, brief: Look at PagerDuty for incidents., maxUsd: 0.1 }
        create: { type: incident, station: triage }
        limits: { perCheck: 5, perDay: 20, pauseOnBudget: true }
`;

const today = new Date();
const at = (h: number, m: number) => {
  const d = new Date(today);
  d.setHours(h, m, 0, 0);
  return d.toISOString();
};

const base: TriggerCheck = {
  id: 'c',
  at: at(14, 10),
  checked_at: at(14, 11),
  state: 'done',
  try: false,
  cost_usd: 0.004,
  reason: null,
  filed: [],
  noted: [],
  skipped: [],
};

const done: TriggerCheck = {
  ...base,
  id: 'c1',
  filed: [{ key: 'Q3T8', title: 'Checkout p95 above 2 s', ref: '#91', reopens: null }],
  noted: [{ key: 'P8XK', ref: '#88', text: 'escalated to P1' }],
  skipped: [{ key: 'N7K1', reason: 'closed on Nov 2: a known flaky monitor' }],
};
const empty: TriggerCheck = { ...base, id: 'c2', at: at(14, 5), cost_usd: 0.002 };
const failed: TriggerCheck = {
  ...base,
  id: 'c3',
  at: at(13, 55),
  state: 'failed',
  reason: 'PagerDuty answered 429. The next check tries again.',
  cost_usd: 0.001,
};

const detail: IntakeTriggerDetail = {
  trigger: {
    name: 'new-incidents',
    enabled: true,
    source: 'schedule',
    on: 'schedule',
    when: '*/5 * * * *, UTC',
    create: { type: 'incident', station: 'triage', lane: null, expedite: false },
    days: [0, 0, 0, 0, 0, 1, 2],
    problem: null,
    check: { agent: 'watcher', max_usd: 0.1 },
  },
  week: { created: 2, merged: 0, spend_usd: 0.41 },
  events: [],
  memory: {
    open: 2,
    open_items: [],
    closed: 3,
    closed_keys: [],
    state: { cursor: 'x' },
    checked_up_to: at(14, 10),
  },
} as IntakeTriggerDetail;

const mocked = {
  getIntakeTrigger: vi.mocked(intake.getIntakeTrigger),
  listTriggerChecks: vi.mocked(intake.listTriggerChecks),
  tryTriggerCheck: vi.mocked(intake.tryTriggerCheck),
  clearTriggerState: vi.mocked(intake.clearTriggerState),
};

beforeEach(() => {
  vi.mocked(definitions.getFactory).mockResolvedValue(factory);
  vi.mocked(definitions.getDraft).mockResolvedValue({ id: 'd', revision: 1, change_count: 0 } as never);
  vi.mocked(definitions.getVersionFile).mockResolvedValue({ text: file, sha256: 'sha' });
  mocked.getIntakeTrigger.mockResolvedValue(detail);
  mocked.listTriggerChecks.mockResolvedValue({ checks: [done, empty, failed] });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <TriggerPage />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe('a trigger with a check', () => {
  it('shows When, Check, Create and Limits, and no Only if', async () => {
    renderPage();
    expect(await screen.findByRole('heading', { level: 1, name: 'New incidents' })).toBeTruthy();
    for (const name of ['When', 'Check', 'Create', 'Limits']) expect(screen.getByRole('region', { name })).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Only if' })).toBeNull();

    const check = screen.getByRole('region', { name: 'Check' });
    expect((within(check).getByRole('combobox', { name: 'Agent' }) as HTMLSelectElement).value).toBe('watcher');
    expect(within(within(check).getByRole('list', { name: 'Tools' })).getByText('read')).toBeTruthy();
    expect((within(check).getByRole('textbox', { name: /Brief/ }) as HTMLTextAreaElement).value).toBe(
      'Look at PagerDuty for incidents.',
    );
    expect(within(check).getByText('2 open items')).toBeTruthy();
    expect(within(check).getByText('3 closed this week')).toBeTruthy();
    expect(within(check).getByText(/Checked up to/)).toBeTruthy();

    const limits = screen.getByRole('region', { name: 'Limits' });
    expect((within(limits).getByRole('spinbutton', { name: 'Items a check' }) as HTMLInputElement).value).toBe('5');
    expect((within(limits).getByRole('spinbutton', { name: 'Items a day' }) as HTMLInputElement).value).toBe('20');
    expect((within(limits).getByRole('spinbutton', { name: /Most a check spends/ }) as HTMLInputElement).value).toBe(
      '0.10',
    );
  });

  it('sums today’s checks, items and cost beside the title', async () => {
    renderPage();
    await screen.findByRole('heading', { level: 1 });
    await screen.findByText(/Today:/);
    const line = screen.getByText(/Today:/).closest('p') as HTMLElement;
    const spoken = line.cloneNode(true) as HTMLElement;
    for (const hidden of spoken.querySelectorAll('[aria-hidden="true"]')) hidden.remove();
    expect(spoken.textContent?.replace(/\s+/g, ' ').trim()).toBe('Today: 3 checks, 1 item, $0.01');
  });

  it('logs a done check with what it filed, noted and skipped, a nothing-new check and a failed one', async () => {
    renderPage();
    await screen.findByRole('heading', { level: 1 });
    const log = await screen.findByRole('region', { name: 'Recent checks' });
    const rows = within(log).getAllByRole('listitem');
    expect(rows).toHaveLength(3);

    const first = within(rows[0] as HTMLElement);
    expect(first.getByText('Filed #91')).toBeTruthy();
    expect(first.getByText(/Q3T8 Checkout p95 above 2 s/)).toBeTruthy();
    expect(first.getByText(/P8XK on/)).toBeTruthy();
    expect(first.getByText(/escalated to P1/)).toBeTruthy();
    expect(first.getByText(/N7K1 closed on Nov 2: a known flaky monitor/)).toBeTruthy();
    expect(first.getByText('$0.004')).toBeTruthy();
    expect(first.getAllByRole('link').map((a) => a.getAttribute('href'))).toEqual(['/items/91', '/items/88']);

    expect(within(rows[1] as HTMLElement).getByText('Nothing new')).toBeTruthy();
    const third = within(rows[2] as HTMLElement);
    expect(third.getByText('Failed')).toBeTruthy();
    expect(third.getByText('PagerDuty answered 429. The next check tries again.')).toBeTruthy();
  });

  it('says so when no check has run yet', async () => {
    mocked.listTriggerChecks.mockResolvedValue({ checks: [] });
    renderPage();
    expect(await screen.findByText('No checks yet')).toBeTruthy();
  });

  it('shows text from an agent as text, never as markup', async () => {
    const hostile = '<img src=x onerror="alert(1)">';
    mocked.listTriggerChecks.mockResolvedValue({
      checks: [{ ...done, skipped: [{ key: 'K1', reason: hostile }] }],
    });
    renderPage();
    const log = await screen.findByRole('region', { name: 'Recent checks' });
    expect(within(log).getByText(/K1 <img/)).toBeTruthy();
    expect(log.querySelector('img')).toBeNull();
  });

  it('marks a try as a try, with what it would have done', async () => {
    mocked.listTriggerChecks.mockResolvedValue({
      checks: [{ ...done, id: 't1', try: true, filed: [{ key: 'Q3T8', title: 'Checkout', ref: null, reopens: null }] }],
    });
    renderPage();
    const log = await screen.findByRole('region', { name: 'Recent checks' });
    expect(within(log).getByText('Try')).toBeTruthy();
    expect(within(log).getByText('Would file 1')).toBeTruthy();
  });

  it('clears what it remembers and reads the trigger again', async () => {
    mocked.clearTriggerState.mockResolvedValue(undefined);
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: 'Clear what it remembers' }));
    await waitFor(() => expect(mocked.clearTriggerState).toHaveBeenCalledWith('org-1', 'factory-1', 'new-incidents'));
    await waitFor(() => expect(mocked.getIntakeTrigger).toHaveBeenCalledTimes(2));
  });

  it('says why Clear was refused while a check is running (409)', async () => {
    mocked.clearTriggerState.mockRejectedValue(
      new ApiError({ title: 'Conflict', status: 409, code: 'still-checking' }),
    );
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: 'Clear what it remembers' }));
    expect(await screen.findByText('A check is queued or running. Clear when it is done.')).toBeTruthy();
  });

  it('turns Try a check off while a check is queued or running', async () => {
    mocked.listTriggerChecks.mockResolvedValue({ checks: [{ ...base, id: 'r', state: 'running', checked_at: null }] });
    renderPage();
    await screen.findByRole('region', { name: 'Recent checks' });
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Try a check' }).hasAttribute('disabled')).toBe(true),
    );
    expect(screen.getByText('A check is queued or running')).toBeTruthy();
  });

  it('keeps Save off while the brief is blank', async () => {
    renderPage();
    await screen.findByRole('heading', { level: 1 });
    const brief = screen.getByRole('textbox', { name: /Brief/ });
    fireEvent.change(brief, { target: { value: '  ' } });
    expect(screen.getByRole('button', { name: 'Save changes' }).hasAttribute('disabled')).toBe(true);
    fireEvent.change(brief, { target: { value: 'Look at it again.' } });
    expect(screen.getByRole('button', { name: 'Save changes' }).hasAttribute('disabled')).toBe(false);
  });
});
