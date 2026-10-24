import { QueryClient } from '@tanstack/react-query';
import { describe, expect, test, vi } from 'vitest';
import { defKeys, keys } from '../api/queries';
import type { DefinitionEvent, StreamEvent } from '../api/types';
import { definitionInvalidations, invalidationsFor, isOverviewKey, resyncInvalidations } from './invalidation';

const acme = '019a1c00-0000-7000-8000-0000000000aa';
const labs = '019a1c00-0000-7000-8000-0000000000bb';
const goal = '6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60';

// This tab saved the goal factory's draft up to revision 4: the stream's events of those are echoes.
const saved = vi.hoisted(() => ({ factory: '6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60', revision: 4 }));
vi.mock(import('../api/definitions'), async (original) => ({
  ...(await original()),
  savedHere: (factory: string, revision: number) => factory === saved.factory && revision <= saved.revision,
}));
const sre = '0b7d9e21-58a4-4f0e-b6c3-2a9d8e7f6a51';
const item12 = '00000000-0000-4000-8000-00000000040c';

// A cache holding the same screens for two organizations, plus a factory key that must not refetch
// while agents work (a later phase's definition draft lives under the factory's prefix).
function cache() {
  const client = new QueryClient();
  for (const org of [acme, labs]) {
    client.setQueryData(keys.factories(org), []);
    client.setQueryData(keys.attention(org), {});
    client.setQueryData(keys.inbox(org), {});
    client.setQueryData(keys.inbox(org, goal), {});
    client.setQueryData(keys.overview(org, '7d'), {});
    client.setQueryData(keys.board(org, goal), {});
    client.setQueryData(keys.metrics(org, goal, '7d'), {});
    client.setQueryData(keys.items(org, goal, { state: 'waiting_human' }), {});
    client.setQueryData([...keys.factory(org, goal), 'draft'], {});
    client.setQueryData(keys.board(org, sre), {});
    client.setQueryData(keys.item(org, 'goal#12'), {});
    client.setQueryData(keys.item(org, item12), {});
    client.setQueryData(keys.events(org, 'goal#12'), []);
    client.setQueryData(keys.item(org, 'goal#7'), {});
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

const event = (kind: string): StreamEvent => ({
  org_id: acme,
  item_id: item12,
  ref: 'goal#12',
  factory_id: goal,
  kind,
  station: 'approve-spec',
  state: 'waiting_human',
  at: '2026-10-24T10:00:00Z',
});

describe('invalidationsFor an item event', () => {
  test("touches the organization's counts, the factory's work and the item, in its organization only", async () => {
    const stale = await staleAfter(cache(), invalidationsFor(acme, event('approved')));

    expect(stale).toEqual(
      expect.arrayContaining([
        keys.attention(acme),
        keys.inbox(acme),
        keys.inbox(acme, goal),
        keys.overview(acme, '7d'),
        keys.board(acme, goal),
        keys.metrics(acme, goal, '7d'),
        keys.items(acme, goal, { state: 'waiting_human' }),
        keys.item(acme, 'goal#12'),
        keys.item(acme, item12),
        keys.events(acme, 'goal#12'),
      ]),
    );
    expect(stale).not.toContainEqual(keys.board(acme, sre));
    expect(stale).not.toContainEqual(keys.item(acme, 'goal#7'));
    expect(stale).not.toContainEqual(keys.factories(acme));
    expect(stale.filter((k) => k[1] === labs)).toEqual([]);
  });

  test("never re-reads the factory's whole prefix", async () => {
    const stale = await staleAfter(cache(), invalidationsFor(acme, event('moved')));

    expect(stale).not.toContainEqual([...keys.factory(acme, goal), 'draft']);
  });

  test("a tool call re-reads only the factory's board of its work", async () => {
    const stale = await staleAfter(cache(), invalidationsFor(acme, event('tool_call')));

    expect(stale).toContainEqual(keys.board(acme, goal));
    expect(stale).toContainEqual(keys.item(acme, 'goal#12'));
    expect(stale).not.toContainEqual(keys.metrics(acme, goal, '7d'));
    expect(stale).not.toContainEqual(keys.items(acme, goal, { state: 'waiting_human' }));
  });

  test('marks the overview keys, which the stream batches separately', () => {
    const overview = invalidationsFor(acme, event('approved')).filter(isOverviewKey);
    expect(overview).toEqual([['org', acme, 'overview']]);
  });
});

describe('resyncInvalidations', () => {
  test('re-reads everything of the organization and nothing of another', async () => {
    const stale = await staleAfter(cache(), resyncInvalidations(acme));

    expect(stale).toHaveLength(14);
    expect(stale.every((k) => k[0] === 'org' && k[1] === acme)).toBe(true);
  });
});

// The definitions screens' cache for acme's goal factory, beside its work.
function definitionCache() {
  const client = new QueryClient();
  client.setQueryData(keys.factories(acme), []);
  client.setQueryData(keys.attention(acme), {});
  client.setQueryData(keys.overview(acme, '7d'), {});
  client.setQueryData(keys.board(acme, goal), {});
  client.setQueryData(keys.metrics(acme, goal, '7d'), {});
  client.setQueryData(defKeys.factory(acme, goal), {});
  client.setQueryData(defKeys.draft(acme, goal), {});
  client.setQueryData(defKeys.draftModel(acme, goal), {});
  client.setQueryData(defKeys.draftFile(acme, goal, 'factory.yaml'), {});
  client.setQueryData(defKeys.validation(acme, goal, 4), {});
  client.setQueryData(defKeys.versions(acme, goal), {});
  client.setQueryData(defKeys.version(acme, goal, '8'), {});
  client.setQueryData(defKeys.compare(acme, goal, 'base', 'draft'), {});
  client.setQueryData(defKeys.versionFile(acme, 'a1b2'), '');
  client.setQueryData(defKeys.templates(acme), []);
  client.setQueryData(defKeys.draft(acme, sre), {});
  return client;
}

const definition = (e: Partial<DefinitionEvent>): DefinitionEvent => ({
  org_id: acme,
  factory_id: goal,
  kind: 'draft',
  ...e,
});

describe('definitionInvalidations', () => {
  test("a save elsewhere re-reads the factory's draft and its chip, not its work or other drafts", async () => {
    const stale = await staleAfter(
      definitionCache(),
      definitionInvalidations(acme, definition({ revision: saved.revision + 1, by: 'user:local' })),
    );

    expect(stale).toEqual(
      expect.arrayContaining([
        defKeys.draft(acme, goal),
        defKeys.draftModel(acme, goal),
        defKeys.draftFile(acme, goal, 'factory.yaml'),
        defKeys.factory(acme, goal),
        keys.factories(acme),
      ]),
    );
    expect(stale).not.toContainEqual(keys.board(acme, goal));
    expect(stale).not.toContainEqual(defKeys.versions(acme, goal));
    expect(stale).not.toContainEqual(defKeys.draft(acme, sre));
    expect(stale).not.toContainEqual(defKeys.versionFile(acme, 'a1b2'));
  });

  test("the echo of this tab's own save re-reads nothing", async () => {
    const stale = await staleAfter(
      definitionCache(),
      definitionInvalidations(acme, definition({ revision: saved.revision })),
    );

    expect(stale).toEqual([]);
  });

  test('a publish re-reads what runs: versions, the draft, comparisons, the board and the overviews', async () => {
    const stale = await staleAfter(
      definitionCache(),
      definitionInvalidations(acme, definition({ kind: 'current', number: 9 })),
    );

    expect(stale).toEqual(
      expect.arrayContaining([
        defKeys.versions(acme, goal),
        defKeys.version(acme, goal, '8'),
        defKeys.draft(acme, goal),
        defKeys.compare(acme, goal, 'base', 'draft'),
        keys.board(acme, goal),
        keys.overview(acme, '7d'),
        keys.factories(acme),
      ]),
    );
    expect(stale).not.toContainEqual(keys.metrics(acme, goal, '7d'));
    expect(stale).not.toContainEqual(defKeys.versionFile(acme, 'a1b2'));
  });

  test('a factory created or deleted re-reads the list, attention and overviews', async () => {
    const stale = await staleAfter(definitionCache(), definitionInvalidations(acme, definition({ kind: 'factory' })));

    expect(stale).toEqual(
      expect.arrayContaining([keys.factories(acme), keys.attention(acme), keys.overview(acme, '7d')]),
    );
    expect(stale).not.toContainEqual(defKeys.draft(acme, goal));
  });

  test('ignores data that is no definition event', () => {
    expect(definitionInvalidations(acme, { kind: 'draft' })).toEqual([]);
    expect(definitionInvalidations(acme, null)).toEqual([]);
  });
});
