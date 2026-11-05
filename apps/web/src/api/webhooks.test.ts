// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from './client';
import { setSessionHandlers } from './session';
import { createWebhook, listWebhookDeliveries, patchWebhook } from './webhooks';

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

describe('creating a webhook through the session', () => {
  it('asks for a fresh sign-in on a stale session, then sends the request once more', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(
        json(403, { code: 'reauth-required', title: 'Sign in again', status: 403 }, 'application/problem+json'),
      )
      .mockResolvedValueOnce(json(201, { id: 'hook-1', name: 'github-acme', secret_value: zeros }));
    vi.stubGlobal('fetch', fetch);
    const reauth = vi.fn(async () => true);
    setSessionHandlers({ reauth });

    const rotated = await createWebhook('org-1', { name: 'github-acme', sender: 'github' });

    expect(reauth).toHaveBeenCalledTimes(1);
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(rotated.secret_value).toBe(zeros);
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

    await expect(createWebhook('org-1', { name: 'github-acme', sender: 'github' })).rejects.toBeInstanceOf(ApiError);
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

    await expect(createWebhook('org-1', { name: 'github-acme', sender: 'github' })).rejects.toMatchObject({
      code: 'reauth-required',
    });
  });
});

describe('the organization routes', () => {
  it('lists a page of deliveries after the previous page’s cursor', async () => {
    const fetch = vi.fn().mockResolvedValue(json(200, { deliveries: [], next_after: null }));
    vi.stubGlobal('fetch', fetch);

    await listWebhookDeliveries('org-1', 'github-acme', { limit: 20, after: 'cursor-1' });

    const [request] = fetch.mock.calls[0] as [Request];
    const url = new URL(request.url);
    expect(url.pathname).toBe('/v1/orgs/org-1/webhooks/github-acme/deliveries');
    expect(url.searchParams.get('limit')).toBe('20');
    expect(url.searchParams.get('after')).toBe('cursor-1');
  });

  it('keeps the triggers a 409 webhook-in-use names', async () => {
    const triggers = [{ factory: 'f-1', factory_name: 'sre', trigger: 'check-fails' }];
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          json(409, { code: 'webhook-in-use', title: 'In use', status: 409, triggers }, 'application/problem+json'),
        ),
    );

    await expect(patchWebhook('org-1', 'github-acme', { close: ['f-1'] })).rejects.toMatchObject({
      code: 'webhook-in-use',
      problem: { triggers },
    });
  });
});
