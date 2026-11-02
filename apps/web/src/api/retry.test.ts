import { describe, expect, it } from 'vitest';
import { ApiError } from './client';
import { shouldRetry } from './retry';

const err = (status: number, code?: string) => new ApiError({ title: 'x', status, ...(code ? { code } : {}) });

describe('shouldRetry', () => {
  it('does not retry a 4xx', () => {
    expect(shouldRetry(0, err(404))).toBe(false);
    expect(shouldRetry(0, err(409))).toBe(false);
  });

  it('does not retry a 501 or an unavailable code', () => {
    expect(shouldRetry(0, err(501))).toBe(false);
    expect(shouldRetry(0, err(503, 'judges-unavailable'))).toBe(false);
    expect(shouldRetry(0, err(501, 'evals-unavailable'))).toBe(false);
    expect(shouldRetry(0, err(503, 'jonah-unavailable'))).toBe(false);
  });

  it('retries a 502, a 503 and a network error up to twice', () => {
    expect(shouldRetry(0, err(502))).toBe(true);
    expect(shouldRetry(1, err(503))).toBe(true);
    expect(shouldRetry(0, new TypeError('fetch failed'))).toBe(true);
    expect(shouldRetry(2, err(503))).toBe(false);
    expect(shouldRetry(2, new TypeError('fetch failed'))).toBe(false);
  });
});
