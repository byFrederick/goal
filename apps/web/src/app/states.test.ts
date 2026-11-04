import { ApiError } from '@goal/api-client';
import { CancelledError } from '@tanstack/react-query';
import { describe, expect, it } from 'vitest';
import { NetworkError } from '../api/client';
import { loadFailure } from './states';

describe('the error screen', () => {
  it('loads again after a cancelled fetch instead of saying goald is down', () => {
    expect(loadFailure(new CancelledError())).toBe('interrupted');
  });

  it('says goald is not answering when the request never got an answer', () => {
    expect(loadFailure(new NetworkError(new TypeError('Failed to fetch')))).toBe('unreachable');
  });

  it.each([502, 503, 504])('says goald is not answering on a %i', (status) => {
    expect(loadFailure(new ApiError({ title: 'Bad Gateway', status }))).toBe('unreachable');
  });

  it('does not blame goald for a crash in the page', () => {
    expect(loadFailure(new TypeError("Cannot read properties of undefined (reading 'x')"))).toBe('crashed');
  });

  it("shows goald's refusal", () => {
    expect(loadFailure(new ApiError({ title: 'Forbidden', status: 403 }))).toBe('refused');
  });
});
