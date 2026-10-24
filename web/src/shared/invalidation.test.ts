import { QueryClient } from '@tanstack/react-query';
import { describe, expect, test } from 'vitest';
import { keys } from '../api/queries';
import { itemInvalidations, resyncInvalidations } from './invalidation';

const acme = '019a1c00-0000-7000-8000-0000000000aa';
const labs = '019a1c00-0000-7000-8000-0000000000bb';

// A cache holding the same screens for two organizations.
function cache() {
  const client = new QueryClient();
  for (const org of [acme, labs]) {
    client.setQueryData(keys.factories(org), []);
    client.setQueryData(keys.board(org, 'goal'), {});
    client.setQueryData(keys.board(org, 'sre'), {});
    client.setQueryData(keys.item(org, 'goal#12'), {});
    client.setQueryData(keys.events(org, 'goal#12'), []);
  }
  return client;
}

/** Invalidates keys (without refetching) and returns the keys of the queries now invalidated. */
async function staleAfter(client: QueryClient, invalidate: readonly (readonly unknown[])[]) {
  await Promise.all(invalidate.map((queryKey) => client.invalidateQueries({ queryKey, refetchType: 'none' })));
  return client
    .getQueryCache()
    .getAll()
    .filter((q) => q.state.isInvalidated)
    .map((q) => q.queryKey);
}

describe('stream invalidation', () => {
  test("an item event touches its factory's work and the item, in its organization only", async () => {
    const stale = await staleAfter(cache(), itemInvalidations(acme, 'goal', ['goal#12']));

    expect(stale).toEqual(
      expect.arrayContaining([keys.board(acme, 'goal'), keys.item(acme, 'goal#12'), keys.events(acme, 'goal#12')]),
    );
    expect(stale).not.toContainEqual(keys.board(acme, 'sre'));
    expect(stale.filter((k) => k[1] === labs)).toEqual([]);
  });

  test('a resync re-reads everything of the organization and nothing of another', async () => {
    const stale = await staleAfter(cache(), resyncInvalidations(acme));

    expect(stale).toHaveLength(5);
    expect(stale.every((k) => k[0] === 'org' && k[1] === acme)).toBe(true);
  });
});
