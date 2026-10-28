import type { Problem } from '@goal/api-client';
import { describe, expect, it } from 'vitest';
import { ApiError } from '../../api/client';
import { askFailure, isOpenRequest } from './draft-requests';

const refused = (code: string, detail?: string) =>
  new ApiError({ title: 'Conflict', status: 409, code, detail } as unknown as Problem);

describe('askFailure', () => {
  it('says a request is already running for draft-request-open', () => {
    expect(askFailure(refused('draft-request-open'))).toBe(
      'A request is already running. Wait for it to finish, then ask again.',
    );
  });

  it('shows the detail of yaml-invalid rather than the already-running sentence', () => {
    const text = askFailure(refused('yaml-invalid', 'factory.yaml does not parse at line 3'));
    expect(text).toBe('Not asked: factory.yaml does not parse at line 3');
    expect(text).not.toContain('already running');
  });

  it('says goald is not answering when the failure is not an API refusal', () => {
    expect(askFailure(new Error('offline'))).toMatch(/not answering/);
  });
});

describe('isOpenRequest', () => {
  it('is true for a queued or running request only', () => {
    expect(isOpenRequest({ status: 'queued' })).toBe(true);
    expect(isOpenRequest({ status: 'running' })).toBe(true);
    expect(isOpenRequest({ status: 'done' })).toBe(false);
    expect(isOpenRequest({ status: 'failed' })).toBe(false);
    expect(isOpenRequest(undefined)).toBe(false);
  });
});
