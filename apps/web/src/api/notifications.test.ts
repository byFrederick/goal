// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { putNotificationPreferences } from './notifications';
import type { NotificationPreferences } from './types';

// goald's client at an absolute origin, so the request is a Request fetch can be stubbed for.
vi.mock(import('./client'), async (original) => {
  const { createGoalClient } = await import('@goal/api-client');
  const { sessionHandlers, withSession } = await import('./session');
  return {
    ...(await original()),
    goal: createGoalClient('http://goald.test', { fetch: withSession((request) => fetch(request), sessionHandlers) }),
  };
});

const prefs = (timeZone: string): NotificationPreferences => ({
  events: {
    approval: { in_goal: true, email: false, slack: true },
    question: { in_goal: true, email: false, slack: true },
    andon: { in_goal: true, email: true, slack: true },
    budget: { in_goal: true, email: true, slack: false },
    digest: { in_goal: false, email: true, slack: false },
  },
  email: 'freddy@example.com',
  time_zone: timeZone,
});

/** Stubs fetch to answer with the body it was sent, and returns what was sent. */
function echo() {
  const sent: unknown[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (request: Request) => {
      const body = await request.json();
      sent.push(body);
      return new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } });
    }),
  );
  return sent;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('saving the notification preferences', () => {
  it('sends the browser’s time zone while the saved one is UTC', async () => {
    vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockReturnValue({
      timeZone: 'Europe/Berlin',
    } as Intl.ResolvedDateTimeFormatOptions);
    const sent = echo();
    await putNotificationPreferences(prefs('UTC'));
    expect(sent[0]).toEqual({ ...prefs('UTC'), time_zone: 'Europe/Berlin' });
  });

  it('keeps a time zone that was saved', async () => {
    const sent = echo();
    await putNotificationPreferences(prefs('Asia/Tokyo'));
    expect(sent[0]).toEqual(prefs('Asia/Tokyo'));
  });
});
