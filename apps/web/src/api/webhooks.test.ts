// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from './client';
import { setSessionHandlers } from './session';
import { rotateWebhookSecret } from './webhooks';

// The secret is an all-zero fixture. The request goes through the real client and session wrapper, with
// fetch replaced: what a 403 reauth-required and a 401 do to a secret-returning request.
const zeros = '0'.repeat(64);

// The client's base is goald's origin, as the browser has it; fetch is the global one, which each test stubs.
vi.mock(import('./client'), async (original) => {
  const { createGoalClient } = await import('@goal/api-client');
  const { sessionHandlers, withSession } = await import('./session');
  return {
    ...(await original()),
    goal: createGoalClient('http://goald.test', { fetch: withSession((request) => fetch(request), sessionHandlers) }),
  };
});

function json(status: number, body: unknown, type = 'application/json'): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': type } });
}

afterEach(() => {
  vi.unstubAllGlobals();
  setSessionHandlers({ reauth: async () => false, unauthenticated: () => {}, forbidden: () => {} });
});

describe('rotating a webhook secret through the session', () => {
  it('asks for a fresh sign-in on a stale session, then sends the rotation once more', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(
        json(403, { code: 'reauth-required', title: 'Sign in again', status: 403 }, 'application/problem+json'),
      )
      .mockResolvedValueOnce(json(200, { id: 'cf-endpoint', name: 'cf-deploys', secret: zeros }));
    vi.stubGlobal('fetch', fetch);
    const reauth = vi.fn(async () => true);
    setSessionHandlers({ reauth });

    const rotated = await rotateWebhookSecret('org-1', 'software-id', 'cf-endpoint');

    expect(reauth).toHaveBeenCalledTimes(1);
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(rotated.secret).toBe(zeros);
  });

  it('asks the person to sign in when the session is gone', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          json(401, { code: 'unauthenticated', title: 'Sign in', status: 401 }, 'application/problem+json'),
        ),
    );
    const unauthenticated = vi.fn();
    setSessionHandlers({ unauthenticated });

    await expect(rotateWebhookSecret('org-1', 'software-id', 'cf-endpoint')).rejects.toBeInstanceOf(ApiError);
    expect(unauthenticated).toHaveBeenCalledTimes(1);
  });

  it('reports the refusal when the person does not confirm it is them', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          json(403, { code: 'reauth-required', title: 'Sign in again', status: 403 }, 'application/problem+json'),
        ),
    );
    setSessionHandlers({ reauth: async () => false });

    await expect(rotateWebhookSecret('org-1', 'software-id', 'cf-endpoint')).rejects.toMatchObject({
      code: 'reauth-required',
    });
  });
});
