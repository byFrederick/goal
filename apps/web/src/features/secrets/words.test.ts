// The Secrets screens' words and counts, with a fixed clock: the expiry text's states, the banner's
// headline, when each secret was last used, the activity sentences and the import's counts.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import type { AuditEntry, Secret } from '../../api/types';
import {
  activityText,
  deleteReason,
  expiringHeadline,
  expiringSoon,
  expiryOf,
  importCount,
  importFormat,
  lastUsedText,
  nameProblem,
  placesText,
  refusalText,
  reminderText,
  usedAtLabel,
} from './words';

const NOW = Date.parse('2026-10-27T12:00:00Z');
const DAY = 86_400_000;
const iso = (ms: number) => new Date(ms).toISOString();
const by = { kind: 'user' as const, id: 'u', name: 'Freddy Cruz' };

const secret = (over: Partial<Secret> = {}): Secret => ({
  id: 'secret-1',
  name: 'cloudflare-deploy',
  source: 'value',
  description: '',
  scope: { all_factories: true, factories: [] },
  expires_at: null,
  remind_days: 0,
  last_used_at: null,
  uses: [],
  created_by: by,
  created_at: iso(NOW - 90 * DAY),
  updated_by: by,
  updated_at: iso(NOW - 90 * DAY),
  ...over,
});

const entry = (action: string, data: Record<string, unknown> = {}): AuditEntry => ({
  id: 1,
  at: iso(NOW),
  actor: by,
  action,
  target: { kind: 'secret', id: 'secret-1' },
  data,
});

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('expiryOf', () => {
  it('says never when there is no expiry', () => {
    expect(expiryOf(secret(), NOW)).toEqual({ state: 'never' });
  });

  it('gives the date with its year outside the reminder window', () => {
    expect(expiryOf(secret({ expires_at: iso(NOW + 46 * DAY), remind_days: 14 }), NOW)).toEqual({
      state: 'set',
      date: 'Dec 12, 2026',
    });
  });

  it('gives the days left inside the reminder window, opening it exactly remind_days before', () => {
    expect(expiryOf(secret({ expires_at: iso(NOW + 3 * DAY), remind_days: 7 }), NOW)).toMatchObject({
      state: 'reminder',
      days: 3,
    });
    expect(expiryOf(secret({ expires_at: iso(NOW + 14 * DAY), remind_days: 14 }), NOW).state).toBe('reminder');
  });

  it('says expired once the expiry has passed', () => {
    expect(expiryOf(secret({ expires_at: iso(NOW - DAY) }), NOW).state).toBe('expired');
  });
});

describe('the expiring banner', () => {
  it('names the secrets in their window or expired, and reads the headline in words', () => {
    const soon = secret({ expires_at: iso(NOW + 3 * DAY), remind_days: 7 });
    const far = secret({ name: 'other', expires_at: iso(NOW + 60 * DAY), remind_days: 7 });

    expect(expiringSoon([soon, far], NOW)).toEqual([soon]);
    expect(expiringHeadline(soon, NOW)).toBe('cloudflare-deploy expires in 3 days');
    expect(expiringHeadline(secret({ expires_at: iso(NOW - DAY) }), NOW)).toBe(
      'cloudflare-deploy expired Oct 26, 2026',
    );
  });
});

describe('lastUsedText', () => {
  it('says never for a secret no run opened, the age within a day and the day after', () => {
    expect(lastUsedText(secret(), NOW)).toBe('Never used');
    expect(lastUsedText(secret({ last_used_at: iso(NOW - 120_000) }), NOW)).toBe('2 min ago');
    expect(lastUsedText(secret({ last_used_at: iso(NOW - 3 * DAY) }), NOW)).toBe('Oct 24');
  });
});

describe('uses', () => {
  it('labels a use by its name and kind, and counts places', () => {
    expect(usedAtLabel({ kind: 'definition', name: 'software' })).toBe('software · factory definition');
    expect(usedAtLabel({ kind: 'slack', name: 'Slack' })).toBe('Slack');
    expect(placesText(0)).toBe('nowhere');
    expect(placesText(1)).toBe('in 1 place');
    expect(deleteReason(5)).toBe('Remove it from the 5 places above first. Until then it can’t be deleted.');
  });
});

describe('the name rule', () => {
  it('accepts a secret name and names what is wrong with the rest', () => {
    expect(nameProblem('sentry-token')).toBeUndefined();
    expect(nameProblem('')).toMatch(/name/);
    expect(nameProblem('Sentry_Token')).toMatch(/lowercase/);
  });
});

describe('the reminder', () => {
  it('reads in words, and is none without an expiry', () => {
    expect(reminderText(secret({ expires_at: iso(NOW + DAY), remind_days: 14 }))).toBe(
      '14 days before, to owners and admins',
    );
    expect(reminderText(secret({ remind_days: 14 }))).toBe('None');
  });
});

describe('the import', () => {
  it('reads a YAML secrets map as YAML and anything else as .env', () => {
    expect(importFormat('secrets:\n  a-b: x')).toBe('yaml');
    expect(importFormat('A_B=x')).toBe('env');
  });

  it('reads the older top-level credentials map as YAML, but not an indented one', () => {
    expect(importFormat('credentials:\n  a-b: x')).toBe('yaml');
    expect(importFormat('# keys\ncredentials:\n  a-b: x')).toBe('yaml');
    expect(importFormat('other:\n  credentials:\n    a-b: x')).toBe('env');
  });

  it('counts the new names, and the existing ones too when they are replaced', () => {
    const found = [{ exists: true }, { exists: false }];
    expect(importCount(found, 'skip')).toBe(1);
    expect(importCount(found, 'replace')).toBe(2);
  });
});

describe('activityText', () => {
  it('reads each action as a sentence with its actor', () => {
    expect(activityText(entry('secret.created'))).toBe('Created by Freddy Cruz');
    expect(activityText(entry('secret.value_replaced'))).toBe('Replaced by Freddy Cruz');
    expect(activityText(entry('secret.used', { run: 147, role: 'frontend-engineer' }))).toBe(
      'Used by run #147, frontend-engineer',
    );
  });
});

describe('refusalText', () => {
  it('says what to set when goald has no master key, and goald’s detail otherwise', () => {
    const unavailable = new ApiError({ title: 'Service Unavailable', status: 503, code: 'connections-unavailable' });
    expect(refusalText(unavailable)).toContain('GOAL_MASTER_KEYS');
    const taken = new ApiError({ title: 'Conflict', status: 409, code: 'secret-in-use', detail: 'used by sre' });
    expect(refusalText(taken)).toBe('used by sre');
    expect(refusalText(new TypeError('fetch failed'))).toBe('goald is not answering. Try again once it is running.');
  });
});
