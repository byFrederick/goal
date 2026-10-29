// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Connection, NotificationPreferences } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { NotificationsTable } from './notifications-section';

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock('sonner', () => ({ toast }));

// goald's client at an absolute origin, so requests are Requests fetch can be stubbed for (the app uses a relative base).
vi.mock(import('../../api/client'), async (original) => {
  const { createGoalClient } = await import('@goal/api-client');
  const { sessionHandlers, withSession } = await import('../../api/session');
  return {
    ...(await original()),
    goal: createGoalClient('http://goald.test', { fetch: withSession((request) => fetch(request), sessionHandlers) }),
  };
});

/** The picture's boxes: approval in Goal and Slack, the andon everywhere, the digest by email. */
const saved: NotificationPreferences = {
  events: {
    approval: { in_goal: true, email: false, slack: true },
    question: { in_goal: true, email: false, slack: true },
    andon: { in_goal: true, email: true, slack: true },
    budget: { in_goal: true, email: true, slack: false },
    digest: { in_goal: false, email: true, slack: false },
  },
  email: '',
  time_zone: 'Europe/Berlin',
};

/** What the fake goald holds, and how it answers: a read or a save can be made to fail or hang. */
const goald = vi.hoisted(() => ({
  prefs: null as NotificationPreferences | null,
  slack: [] as Connection[],
  failRead: false,
  hangRead: false,
  failSave: false,
  requests: [] as { method: string; path: string; kind: string | null; body: unknown }[],
}));

async function answer(input: unknown, init?: RequestInit): Promise<Response> {
  const req = input instanceof Request ? input : undefined;
  const method = req?.method ?? init?.method ?? 'GET';
  const url = new URL(req?.url ?? String(input), 'http://goald.test');
  const body = req
    ? await req
        .clone()
        .json()
        .catch(() => undefined)
    : typeof init?.body === 'string'
      ? JSON.parse(init.body)
      : undefined;
  goald.requests.push({ method, path: url.pathname, kind: url.searchParams.get('kind'), body });
  const json = (status: number, value: unknown) =>
    new Response(JSON.stringify(value), {
      status,
      headers: { 'content-type': status >= 400 ? 'application/problem+json' : 'application/json' },
    });
  const problem = { title: 'Goal is not answering', status: 500 };
  if (url.pathname === '/v1/me/notifications' && method === 'GET') {
    if (goald.hangRead) return new Promise<Response>(() => {});
    return goald.failRead ? json(500, problem) : json(200, goald.prefs);
  }
  if (url.pathname === '/v1/me/notifications' && method === 'PUT') {
    if (goald.failSave) return json(500, problem);
    goald.prefs = body as NotificationPreferences;
    return json(200, goald.prefs);
  }
  if (url.pathname === '/v1/orgs/org-goal/connections') return json(200, { connections: goald.slack });
  return json(404, { title: 'Not found', status: 404 });
}

function renderTable(signedIn = true) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <NotificationsTable org="org-goal" signedIn={signedIn} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

/** The saved PUTs, in the order they were sent. */
const saves = () => goald.requests.filter((r) => r.method === 'PUT');

const box = (name: string) => screen.findByRole('checkbox', { name }) as Promise<HTMLInputElement>;

beforeEach(() => {
  goald.prefs = structuredClone(saved);
  goald.slack = [{ id: 'c-slack', name: 'slack', kind: 'slack' } as Connection];
  goald.failRead = false;
  goald.hangRead = false;
  goald.failSave = false;
  goald.requests.length = 0;
  toast.success.mockClear();
  toast.error.mockClear();
  vi.spyOn(globalThis, 'fetch').mockImplementation(answer);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('the notification table', () => {
  it('shows Loading while the preferences load', async () => {
    goald.hangRead = true;
    renderTable();
    const status = await screen.findByText('Loading');
    expect(status.closest('[aria-busy="true"]')).toBeTruthy();
  });

  it('shows the saved boxes, and None for the digest in Goal', async () => {
    renderTable();
    expect((await box('An agent needs your OK for a write, In Goal')).checked).toBe(true);
    expect((await box('A budget passes 80%, Email')).checked).toBe(true);
    expect((await box('A budget passes 80%, Slack')).checked).toBe(false);
    expect((await box('Daily digest at 08:00, Email')).checked).toBe(true);
    const digest = screen.getByRole('row', { name: /Daily digest at 08:00/ });
    expect(within(digest).getByText('None')).toBeTruthy();
    expect(within(digest).queryByRole('checkbox', { name: 'Daily digest at 08:00, In Goal' })).toBeNull();
  });

  it('shows an error with Retry, and Retry loads the preferences', async () => {
    goald.failRead = true;
    renderTable();
    expect(await screen.findByText('Goal couldn’t load your notification settings.')).toBeTruthy();
    goald.failRead = false;
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect((await box('An agent needs your OK for a write, In Goal')).checked).toBe(true);
  });

  it('saves a changed box with the whole preferences, and keeps the rest', async () => {
    renderTable();
    const slack = await box('A budget passes 80%, Slack');
    fireEvent.click(slack);
    await waitFor(() => expect(slack.checked).toBe(true));
    expect(saves()).toHaveLength(1);
    expect(saves()[0]?.body).toEqual({
      ...saved,
      events: { ...saved.events, budget: { ...saved.events.budget, slack: true } },
      time_zone: 'Europe/Berlin',
    });
  });

  it('keeps both changes when two boxes change one after the other', async () => {
    renderTable();
    fireEvent.click(await box('An agent needs your OK for a write, Email'));
    fireEvent.click(await box('An agent asks you a question, Email'));
    await waitFor(() => expect(saves()).toHaveLength(2));
    const last = saves()[1]?.body as NotificationPreferences;
    expect(last.events.approval.email).toBe(true);
    expect(last.events.question.email).toBe(true);
    await waitFor(() => expect(goald.prefs?.events.question.email).toBe(true));
  });

  it('keeps the saved value and says so when a save fails', async () => {
    goald.failSave = true;
    renderTable();
    const budget = await box('A budget passes 80%, Slack');
    fireEvent.click(budget);
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('Goal couldn’t save that. Check your connection and try again.'),
    );
    expect(budget.checked).toBe(false);
  });

  it('makes the Slack boxes not available with no Slack connection, and changes nothing', async () => {
    goald.slack = [];
    renderTable();
    const approval = await box('An agent needs your OK for a write, Slack');
    expect(approval.checked).toBe(true);
    expect(approval.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(approval);
    expect(approval.checked).toBe(true);
    expect((await box('Daily digest at 08:00, Slack')).getAttribute('aria-disabled')).toBe('true');
    expect(saves()).toHaveLength(0);
  });

  it('asks for the Slack connections of the organization, by kind', async () => {
    renderTable();
    await box('An agent needs your OK for a write, Slack');
    expect(goald.requests.some((r) => r.path === '/v1/orgs/org-goal/connections' && r.kind === 'slack')).toBe(true);
  });

  it('keeps every box not available and requests nothing with sign-in off', async () => {
    renderTable(false);
    const approval = await box('An agent needs your OK for a write, In Goal');
    expect(approval.getAttribute('aria-disabled')).toBe('true');
    expect(approval.checked).toBe(false);
    fireEvent.click(approval);
    expect(approval.checked).toBe(false);
    expect(goald.requests).toHaveLength(0);
  });
});
