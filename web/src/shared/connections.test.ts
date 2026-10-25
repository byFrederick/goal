import { describe, expect, it } from 'vitest';
import type { Connection, ConnectionKind } from '../api/types';
import { monogram, namesText, runEnv, statusLine, testedText, usedText } from './connections';

const connection = (over: Partial<Connection>): Connection => ({
  id: 'c1',
  name: 'github',
  kind: 'github',
  description: '',
  fields: {},
  secrets_set: ['token'],
  status: 'ok',
  used_by: { factories: [], knowledge_sources: [] },
  last_used_at: null,
  last_test: null,
  created_by: { kind: 'user', id: 'local', name: 'Local user' },
  created_at: '2026-10-01T00:00:00Z',
  updated_by: { kind: 'user', id: 'local', name: 'Local user' },
  updated_at: '2026-10-01T00:00:00Z',
  ...over,
});

const kind = (over: Partial<ConnectionKind>): ConnectionKind => ({
  name: 'http-api-key',
  title: 'HTTP API key',
  platform: true,
  fields: [
    { name: 'prefix', title: 'Variable prefix', secret: false, required: true },
    { name: 'header', title: 'Header', secret: false, required: false, default: 'Authorization' },
    { name: 'key', title: 'Key', secret: true, required: true },
  ],
  modes: ['env'],
  model: false,
  testable: true,
  env: ['{{prefix}}_HEADER', '{{prefix}}_KEY', '{{prefix}}_URL'],
  ...over,
});

describe('statusLine', () => {
  it('says the status in words, a failed test with the status it got', () => {
    expect(statusLine(connection({}))).toEqual({ tone: 'ok', text: 'OK' });
    expect(
      statusLine(connection({ status: 'failing', last_test: { ok: false, status: 401, at: '2026-10-24T00:00:00Z' } })),
    ).toEqual({ tone: 'rework', text: 'Test failed: 401' });
    expect(statusLine(connection({ status: 'missing-secret' })).text).toBe('Missing secret');
    expect(statusLine(connection({ status: 'untested' }), kind({})).text).toBe('Never tested');
    expect(statusLine(connection({ status: 'untested' }), kind({ testable: false })).text).toBe('No test');
  });
});

describe('testedText and usedText', () => {
  it('say when it was last tested and used, or never', () => {
    const now = Date.parse('2026-10-24T12:00:00Z');
    const c = connection({ last_test: { ok: true, at: '2026-10-24T10:00:00Z' }, last_used_at: '2026-10-24T11:56:00Z' });
    expect(testedText(c, now)).toBe('Tested 2 h ago');
    expect(usedText(c, now)).toBe('Used 4 min ago');
    expect(testedText(connection({}), now)).toBe('Not tested');
    expect(usedText(connection({}), now)).toBe('Never used');
  });
});

describe('runEnv', () => {
  it('fills the kind’s env names from the fields, and leaves out what has no value', () => {
    expect(runEnv(kind({}), { fields: { prefix: 'STATUS' } })).toEqual(['STATUS_HEADER', 'STATUS_KEY', 'STATUS_URL']);
    expect(runEnv(kind({}), { fields: {} })).toEqual([]);
    expect(runEnv(undefined, { fields: {} })).toEqual([]);
  });
});

describe('monogram', () => {
  it('takes two letters from a kind’s title', () => {
    expect(monogram('GitHub')).toBe('gh');
    expect(monogram('PagerDuty')).toBe('pd');
    expect(monogram('AWS')).toBe('aw');
    expect(monogram('Claude subscription')).toBe('cs');
    expect(monogram('Anthropic API key')).toBe('an');
    expect(monogram('Jira')).toBe('ji');
  });
});

describe('namesText', () => {
  it('joins names as a sentence does', () => {
    expect(namesText([])).toBe('');
    expect(namesText(['goal'])).toBe('goal');
    expect(namesText(['goal', 'sre', 'support'])).toBe('goal, sre and support');
  });
});
