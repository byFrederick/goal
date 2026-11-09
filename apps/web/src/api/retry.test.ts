import { describe, expect, it } from 'vitest';
import { ApiError } from './client';
import { isStarting, isUnavailable, restartDelay, shouldRetry } from './retry';

const failure = (status: number, code?: string) => new ApiError({ title: 'Failed', status, ...(code ? { code } : {}) });

describe('shouldRetry', () => {
  it('does not retry a 503 that says the feature is not built', () => {
    expect(isUnavailable(failure(503, 'jonah-unavailable'))).toBe(true);
    expect(shouldRetry(0, failure(503, 'jonah-unavailable'))).toBe(false);
  });
  it('does not retry a 4xx', () => {
    expect(shouldRetry(0, failure(404))).toBe(false);
  });
  it('leaves a restarting goald to the route boundary', () => {
    expect(isStarting(failure(503, 'goald-starting'))).toBe(true);
    expect(isStarting(failure(503))).toBe(true);
    expect(isStarting(failure(503, 'jonah-unavailable'))).toBe(false);
    expect(isStarting(failure(502))).toBe(false);
    expect(shouldRetry(0, failure(503, 'goald-starting'))).toBe(false);
  });
  it('retries other server errors and network errors twice', () => {
    expect(shouldRetry(1, failure(500))).toBe(true);
    expect(shouldRetry(2, failure(500))).toBe(false);
    expect(shouldRetry(0, new TypeError('network'))).toBe(true);
  });
});

describe('restartDelay', () => {
  it('waits Retry-After, capped at 30 seconds', () => {
    expect(restartDelay(0, 3)).toBe(3_000);
    expect(restartDelay(5, 120)).toBe(30_000);
  });
  it('backs off 2, 4, 8 and then 15 seconds without the header', () => {
    expect([0, 1, 2, 3, 9].map((n) => restartDelay(n))).toEqual([2_000, 4_000, 8_000, 15_000, 15_000]);
  });
});
