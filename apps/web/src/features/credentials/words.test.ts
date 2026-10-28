// The Credentials screen's words and counts, with a fixed clock: the expiry text's states, the
// "expires this week" count, the type counts and when each credential was last used.
import type { Credential } from '@goal/api-client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  countByType,
  expiringText,
  expiringThisWeek,
  expiryOf,
  importCount,
  importFormat,
  lastUsedText,
  nameProblem,
  remindOptions,
  similarName,
  usedByConnections,
  usedByLabels,
} from './words';

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

describe('the name rule', () => {
  it('accepts a credential name and names what is wrong with the rest', () => {
    expect(nameProblem('cloudflare-token-2027')).toBeUndefined();
    expect(nameProblem('ab')).toMatch(/3 to 40/);
    expect(nameProblem('Bad_Name')).toMatch(/3 to 40/);
    expect(nameProblem('1token')).toMatch(/3 to 40/);
    expect(nameProblem('')).toBe('Give the credential a name.');
  });
});

describe('a similar name', () => {
  const existing = ['cloudflare-token', 'linear-key'];

  it('is the existing name before a trailing -suffix, either way round', () => {
    expect(similarName('cloudflare-token-2027', existing)).toBe('cloudflare-token');
    expect(similarName('cloudflare-token', ['cloudflare-token-2027'])).toBe('cloudflare-token-2027');
  });

  it('is none for a name that only starts the same way, or the identical name', () => {
    expect(similarName('cloudflare-tokens', existing)).toBeUndefined();
    expect(similarName('cloud', existing)).toBeUndefined();
    expect(similarName('cloudflare-token', existing)).toBeUndefined();
  });
});

describe('the import', () => {
  it('reads a YAML credentials map as YAML and anything else as .env', () => {
    expect(importFormat('credentials:\n  a-key: x\n')).toBe('yaml');
    expect(importFormat('SENTRY_TOKEN=x\n')).toBe('env');
  });

  it('counts the new names, and the existing ones too when they are replaced', () => {
    const found = [{ exists: false }, { exists: false }, { exists: true }];
    expect(importCount(found, 'skip')).toBe(2);
    expect(importCount(found, 'replace')).toBe(3);
  });

  it('says how many connections use an existing name, in the singular for one', () => {
    expect(usedByConnections(2)).toBe('Used by 2 connections');
    expect(usedByConnections(1)).toBe('Used by 1 connection');
  });
});

describe('the rotation reminder', () => {
  it('offers the days before the expiry, and never', () => {
    expect(remindOptions.map((o) => o.days)).toEqual([14, 7, 30, 0]);
    expect(remindOptions.at(-1)?.label).toBe('Never');
  });
});
