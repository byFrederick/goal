// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { listConnections } from './connections';

// goald's client at an absolute origin, so the request is a Request fetch can be stubbed for.
vi.mock(import('./client'), async (original) => {
  const { createGoalClient } = await import('@goal/api-client');
  const { sessionHandlers, withSession } = await import('./session');
  return {
    ...(await original()),
    goal: createGoalClient('http://goald.test', { fetch: withSession((request) => fetch(request), sessionHandlers) }),
  };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('listing connections', () => {
  it('asks for the connections of one kind with the kind parameter', async () => {
    const fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ connections: [] }), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        }),
    );
    vi.stubGlobal('fetch', fetch);
    await listConnections('org-goal', { kind: 'slack' });
    const url = new URL((fetch.mock.calls[0] as unknown as [Request])[0].url);
    expect(url.pathname).toBe('/v1/orgs/org-goal/connections');
    expect(url.searchParams.get('kind')).toBe('slack');
    expect(url.searchParams.has('factory')).toBe(false);
  });
});
