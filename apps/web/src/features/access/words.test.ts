import { describe, expect, it } from 'vitest';
import type { AccessRow } from '../../api/types';
import { accessSearch } from './search';
import { statusOf } from './words';

const NOW = Date.parse('2026-10-31T12:00:00Z');
const row = (over: Partial<AccessRow>): AccessRow => ({
  kind: 'env',
  name: 'X',
  uses: [],
  secrets: [],
  status: 'ok',
  ...over,
});

describe('statusOf', () => {
  it('says each status in the words of the canvas', () => {
    const at = (ms: number) => new Date(NOW - ms).toISOString();
    expect(statusOf(row({ status: 'tested', tested_at: at(120_000) }), NOW)).toEqual({
      text: 'Tested 2 min ago',
      tone: 'ok',
    });
    expect(statusOf(row({ status: 'ok', last_used_at: at(2 * 86_400_000) }), NOW).text).toBe('Used Oct 29');
    expect(statusOf(row({ status: 'ok' }), NOW).tone).toBe('muted');
    expect(statusOf(row({ status: 'untested' }), NOW).text).toBe('Not tested');
    expect(statusOf(row({ status: 'missing' }), NOW)).toEqual({ text: 'Secret missing', tone: 'bad' });
    expect(statusOf(row({ status: 'failed', test_error: '401' }), NOW).text).toBe('Test failed: 401');
    expect(statusOf(row({ status: 'expiring', expires_at: new Date(NOW + 86_400_000).toISOString() }), NOW).text).toBe(
      'Expires in 1 day',
    );
    expect(
      statusOf(row({ status: 'expired', expires_at: new Date(NOW - 2 * 86_400_000).toISOString() }), NOW).text,
    ).toBe('Expired 2 days ago');
  });
});

describe('accessSearch', () => {
  it('keeps a known view and the text, and drops the rest', () => {
    expect(accessSearch({ by: 'factory', q: 'gh' })).toEqual({ by: 'factory', q: 'gh' });
    expect(accessSearch({ by: 'reach' })).toEqual({});
    expect(accessSearch({ by: 'nope', q: '  ' })).toEqual({});
  });
});
