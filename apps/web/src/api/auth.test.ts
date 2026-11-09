// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';

type FetchOptions = { onError: (ctx: { response: Response }) => void };
const send = vi.hoisted(() => vi.fn());

vi.mock('better-auth/client', () => ({
  createAuthClient: () => ({ emailOtp: { sendVerificationOtp: send } }),
}));
vi.mock('better-auth/client/plugins', () => ({
  deviceAuthorizationClient: () => ({}),
  emailOTPClient: () => ({}),
  organizationClient: () => ({}),
}));

const { AuthFailure, sendCode } = await import('./auth');

/** The client's answer to a refused call: it hands the response to onError and returns the error. */
function answers(status: number, headers: Record<string, string>) {
  send.mockImplementation(async ({ fetchOptions }: { fetchOptions: FetchOptions }) => {
    fetchOptions.onError({ response: new Response(null, { status, headers }) });
    return { data: null, error: { status } };
  });
}

describe('a 503 from /api/auth', () => {
  beforeEach(() => send.mockReset());

  it('becomes a starting failure with the Retry-After seconds', async () => {
    answers(503, { 'Retry-After': '3' });
    const failure = await sendCode('a@example.com').catch((e: unknown) => e);
    expect(failure).toBeInstanceOf(AuthFailure);
    expect(failure).toMatchObject({ kind: 'starting', retryAfterSeconds: 3 });
  });

  it('has no wait when Retry-After is missing', async () => {
    answers(503, {});
    const failure = await sendCode('a@example.com').catch((e: unknown) => e);
    expect(failure).toMatchObject({ kind: 'starting', retryAfterSeconds: undefined });
  });
});
