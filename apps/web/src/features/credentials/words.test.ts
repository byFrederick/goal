// The Credentials screen's words and counts, with a fixed clock: the expiry text's states, the
// "expires this week" count, the type counts and when each credential was last used.
import type { Credential } from '@goal/api-client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { countByType, expiringText, expiringThisWeek, expiryOf, lastUsedText, usedByLabels } from './words';

const NOW = Date.parse('2026-10-27T12:00:00Z');
const DAY = 86_400_000;
const iso = (ms: number) => new Date(ms).toISOString();

const credential = (over: Partial<Credential> = {}): Credential =>
  ({
    id: 'cred-1',
    name: 'cloudflare-token',
    type: 'api_key',
    description: '',
    scope: { all_factories: true, factories: [] },
    agents: {},
    expires_at: null,
    remind_days: 0,
    last_used_at: null,
    used_by: { connections: [], agents: [] },
    created_by: { kind: 'user', id: 'u', name: 'U' },
    created_at: iso(NOW - 90 * DAY),
    updated_by: { kind: 'user', id: 'u', name: 'U' },
    updated_at: iso(NOW - 90 * DAY),
    ...over,
  }) as Credential;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('expiryOf', () => {
  it('says never when there is no expiry', () => {
    expect(expiryOf(credential(), NOW)).toEqual({ state: 'never' });
  });

  it('gives the date outside the reminder window', () => {
    const expiry = expiryOf(credential({ expires_at: iso(NOW + 46 * DAY), remind_days: 14 }), NOW);

    expect(expiry).toEqual({ state: 'set', date: 'Dec 12' });
  });

  it('gives the days left inside the reminder window', () => {
    const expiry = expiryOf(credential({ expires_at: iso(NOW + 2 * DAY), remind_days: 7 }), NOW);

    expect(expiry).toEqual({ state: 'reminder', date: 'Oct 29', days: 2 });
  });

  it('opens the window exactly remind_days before the expiry', () => {
    const atEdge = credential({ expires_at: iso(NOW + 14 * DAY), remind_days: 14 });

    expect(expiryOf(atEdge, NOW).state).toBe('reminder');
    expect(expiryOf(atEdge, NOW - 1).state).toBe('set');
  });

  it('says expired once the expiry has passed', () => {
    expect(expiryOf(credential({ expires_at: iso(NOW - DAY), remind_days: 7 }), NOW)).toEqual({
      state: 'expired',
      date: 'Oct 26',
    });
  });
});

describe('expiringThisWeek', () => {
  it('counts the credentials that expire within seven days, not those further off or already expired', () => {
    const list = [
      credential({ name: 'soon', expires_at: iso(NOW + 2 * DAY) }),
      credential({ name: 'edge', expires_at: iso(NOW + 7 * DAY) }),
      credential({ name: 'later', expires_at: iso(NOW + 8 * DAY) }),
      credential({ name: 'past', expires_at: iso(NOW - DAY) }),
      credential({ name: 'never' }),
    ];

    expect(expiringThisWeek(list, NOW)).toBe(2);
  });

  it('reads the count in words', () => {
    expect(expiringText(1)).toBe('1 expires this week');
    expect(expiringText(2)).toBe('2 expire this week');
  });
});

describe('countByType', () => {
  it('counts each type, zero when there is none', () => {
    const counts = countByType([credential(), credential({ type: 'api_key' }), credential({ type: 'ssh_key' })]);

    expect(counts).toEqual({ api_key: 2, oauth_token: 0, ssh_key: 1, cloud_role: 0, app_key: 0 });
  });
});

describe('lastUsedText', () => {
  it('says never for a credential no run opened', () => {
    expect(lastUsedText(credential(), NOW)).toBe('Never used');
  });

  it('gives the age within a day and the day after', () => {
    expect(lastUsedText(credential({ last_used_at: iso(NOW - 2 * 60_000) }), NOW)).toBe('2 min ago');
    expect(lastUsedText(credential({ last_used_at: iso(NOW - 3 * DAY) }), NOW)).toBe('Oct 24');
  });
});

describe('usedByLabels', () => {
  it('names each connection and each agent that uses the credential', () => {
    const labels = usedByLabels(
      credential({
        used_by: {
          connections: ['github'],
          agents: [{ factory_id: 'f1', agent: 'infra-engineer' }],
        },
      }),
    );

    expect(labels).toEqual(['Connection github', 'infra-engineer']);
  });

  it('is empty for a credential nothing uses', () => {
    expect(usedByLabels(credential())).toEqual([]);
  });
});
