import { describe, expect, it } from 'vitest';
import { ApiError } from '../../api/client';
import type { AuditEntry, ConnectionKind } from '../../api/types';
import { activityText, activityWhen, kindSummary, refusalText } from './words';

const kind = (fields: ConnectionKind['fields']): ConnectionKind => ({
  name: 'k',
  title: 'K',
  platform: true,
  fields,
  modes: ['env'],
  model: false,
  testable: true,
  env: [],
});
const field = (name: string, title: string, required: boolean) => ({ name, title, secret: false, required });

describe('kindSummary', () => {
  it('says what a kind asks for: its required fields, else any of its fields', () => {
    expect(kindSummary(kind([field('token', 'Token', true), field('host', 'Host', false)]))).toBe('Token');
    expect(
      kindSummary(
        kind([field('site', 'Site', true), field('email', 'Email', true), field('token', 'API token', true)]),
      ),
    ).toBe('Site, email and API token');
    expect(kindSummary(kind([field('url', 'URL', false), field('command', 'Command', false)]))).toBe('URL or command');
  });
});

describe('refusalText', () => {
  it('says what to set when goald has no master key, and goald’s detail otherwise', () => {
    const unavailable = new ApiError({ title: 'Service Unavailable', status: 503, code: 'connections-unavailable' });
    expect(refusalText(unavailable)).toContain('GOAL_MASTER_KEYS');
    const taken = new ApiError({ title: 'Conflict', status: 409, code: 'connection-in-use', detail: 'used by sre' });
    expect(refusalText(taken)).toBe('used by sre');
    expect(refusalText(new TypeError('fetch failed'))).toBe('goald is not answering. Try again once it is running.');
  });
});

const entry = (action: string, data: Record<string, unknown>): AuditEntry => ({
  id: 1,
  at: '2026-10-24T12:00:00Z',
  actor: { kind: 'user', id: 'u1', name: 'F. Cruz' },
  action,
  target: { kind: 'connection', id: 'c1' },
  data,
});

describe('activityText', () => {
  it('says each connection change and use in a few words, never a value', () => {
    expect(activityText(entry('connection.created', { name: 'github' }))).toBe('created');
    expect(activityText(entry('connection.updated', { name: 'repos', from: 'github' }))).toBe('renamed from github');
    expect(activityText(entry('connection.secret_replaced', { secrets: ['secret_access_key'] }))).toBe(
      'secret access key replaced',
    );
    expect(activityText(entry('connection.tested', { ok: true }))).toBe('tested, passed');
    expect(activityText(entry('connection.tested', { ok: false, error: 'answered 401' }))).toBe(
      'test failed: answered 401',
    );
    expect(activityText(entry('connection.used', { run_id: 'r1' }))).toBe('used by a run');
  });
});

describe('activityWhen', () => {
  it('is an age within a day, else the day', () => {
    const now = Date.parse('2026-10-24T12:30:00Z');
    expect(activityWhen('2026-10-24T12:00:00Z', now)).toBe('30 min ago');
    expect(activityWhen('2026-10-20T12:00:00Z', now)).toBe('Oct 20');
  });
});
