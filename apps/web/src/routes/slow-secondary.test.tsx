// @vitest-environment jsdom
import { QueryClient } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { agentStatsQuery, boardQuery, itemsQuery, versionModelQuery } from '../api/queries';
import { Route as AgentRoute } from './o.$org.f.$factory.agents.$agent';
import { Route as ItemsRoute } from './o.$org.f.$factory.items.index';

type Loader = (args: Record<string, unknown>) => Promise<unknown> | unknown;

const org = { id: 'org-1' };
const factory = { id: 'fac-1' };

/** Resolves with 'pending' when the loader is still waiting after a few turns of the event loop. */
async function settles(run: () => Promise<unknown> | unknown): Promise<'resolved' | 'pending'> {
  const outcome = Promise.resolve(run()).then(() => 'resolved' as const);
  const timer = new Promise<'pending'>((r) => setTimeout(() => r('pending'), 50));
  return Promise.race([outcome, timer]);
}

afterEach(() => vi.unstubAllGlobals());

describe('loaders wait only for the primary data', () => {
  it('Agent detail resolves while the running version is still loading', async () => {
    // Every request that is not already cached never answers.
    vi.stubGlobal('fetch', () => new Promise(() => {}));
    const queryClient = new QueryClient();
    queryClient.setQueryData(agentStatsQuery(org.id, factory.id, 'writer').queryKey, {} as never);
    const loader = AgentRoute.options.loader as unknown as Loader;

    const outcome = await settles(() =>
      loader({ context: { queryClient, org, factory }, params: { agent: 'writer' } }),
    );

    expect(outcome).toBe('resolved');
    expect(queryClient.getQueryData(versionModelQuery(org.id, factory.id, 'current').queryKey)).toBeUndefined();
  });

  it('Items resolves while the board is still loading', async () => {
    vi.stubGlobal('fetch', () => new Promise(() => {}));
    const queryClient = new QueryClient();
    queryClient.setQueryData(itemsQuery(org.id, factory.id, { limit: 200 }).queryKey, {} as never);
    const loader = ItemsRoute.options.loader as unknown as Loader;

    const outcome = await settles(() => loader({ context: { queryClient, org, factory }, deps: {} }));

    expect(outcome).toBe('resolved');
    expect(queryClient.getQueryData(boardQuery(org.id, factory.id).queryKey)).toBeUndefined();
  });
});
