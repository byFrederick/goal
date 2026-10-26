import { ApiError } from '@goal/api-client';
import { CancelledError } from '@tanstack/react-query';
import { describe, expect, it } from 'vitest';
import { loadFailure } from './states';

describe('the error screen', () => {
  it('loads again after a cancelled fetch instead of saying goald is down', () => {
    expect(loadFailure(new CancelledError())).toBe('interrupted');
  });

  it('says goald is not answering when the request never got an answer', () => {
    expect(loadFailure(new TypeError('Failed to fetch'))).toBe('unreachable');
  });

  it("shows goald's refusal", () => {
    expect(loadFailure(new ApiError({ title: 'Forbidden', status: 403 }))).toBe('refused');
  });
});
