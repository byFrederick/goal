import { describe, expect, it } from 'vitest';
import { ApiError } from './client';
import { isUnavailable, shouldRetry } from './retry';

const failure = (status: number, code?: string) => new ApiError({ title: 'Failed', status, ...(code ? { code } : {}) });

describe('shouldRetry', () => {
  it('does not retry a 503 that says the feature is not built', () => {
    expect(isUnavailable(failure(503, 'judges-unavailable'))).toBe(true);
    expect(shouldRetry(0, failure(503, 'judges-unavailable'))).toBe(false);
    expect(shouldRetry(0, failure(503, 'evals-unavailable'))).toBe(false);
  });
  it('does not retry a 4xx', () => {
    expect(shouldRetry(0, failure(404))).toBe(false);
  });
  it('retries other server errors and network errors twice', () => {
    expect(shouldRetry(0, failure(503))).toBe(true);
    expect(shouldRetry(1, failure(500))).toBe(true);
    expect(shouldRetry(2, failure(500))).toBe(false);
    expect(shouldRetry(0, new TypeError('network'))).toBe(true);
  });
});
