import { QueryClient } from '@tanstack/react-query';
import { describe, expect, test, vi } from 'vitest';
import { defKeys, keys } from '../api/queries';
import type { DefinitionEvent, StreamEvent } from '../api/types';
import {
  connectionInvalidations,
  definitionInvalidations,
  factoryInvalidations,
  heldByMove,
  invalidationsFor,
  isOverviewKey,
  knowledgeInvalidations,
  resyncInvalidations,
} from './invalidation';

const acme = '019a1c00-0000-7000-8000-0000000000aa';
const labs = '019a1c00-0000-7000-8000-0000000000bb';
const goal = '6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60';

// This tab saved revision 4 of the goal factory's draft: the stream's event of it is its echo.
const saved = vi.hoisted(() => ({ factory: '6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60', draft: 'draft-9', revision: 4 }));
vi.mock(import('../api/definitions'), async (original) => ({
  ...(await original()),
  savedHere: (factory: string, draft: string | undefined, revision: number) =>
    factory === saved.factory && draft === saved.draft && revision === saved.revision,
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
    client.setQueryData(keys.metrics(org, goal, {}), {});
    client.setQueryData(keys.touches(org, goal, {}), {});
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
        keys.metrics(acme, goal, {}),
        keys.touches(acme, goal, {}),
        keys.items(acme, goal, { state: 'waiting_human' }),
        keys.item(acme, 'goal#12'),
        keys.item(acme, item12),
        keys.events(acme, 'goal#12'),
      ]),
    );
    expect(stale).not.toContainEqual(keys.board(acme, sre));
    expect(stale).not.toContainEqual(keys.webhooks(acme, sre));
    // A created delivery makes an item, so the factory's webhooks (and their deliveries) refetch with it.
    expect(invalidationsFor(acme, event('approved'))).toContainEqual(keys.webhooks(acme, goal));
    expect(stale).not.toContainEqual(keys.item(acme, 'goal#7'));
    expect(stale).not.toContainEqual(keys.factories(acme));
    expect(stale.filter((k) => k[1] === labs)).toEqual([]);
  });

  test.each(['tool_call', 'tool_result', 'agent_message', 'approved'])(
    'refreshes the organization runs page on a %s event, run progress included',
    async (kind) => {
      const client = cache();
      client.setQueryData(keys.runs(acme, { status: 'running' }), {});
      client.setQueryData(keys.runs(labs, {}), {});
      const stale = await staleAfter(client, invalidationsFor(acme, event(kind)));

      expect(stale).toContainEqual(keys.runs(acme, { status: 'running' }));
      expect(stale.filter((k) => k[1] === labs)).toEqual([]);
    },
  );
  test("a milestone event re-reads the factory's activity, and a tool call does not (its feed lists no run events)", async () => {
    expect(invalidationsFor(acme, event('moved'))).toContainEqual(keys.activity(acme, goal));
    expect(invalidationsFor(acme, event('tool_call'))).not.toContainEqual(keys.activity(acme, goal));
  });

  test("never re-reads the factory's whole prefix", async () => {
    const stale = await staleAfter(cache(), invalidationsFor(acme, event('moved')));

    expect(stale).not.toContainEqual([...keys.factory(acme, goal), 'draft']);
  });

  test("a tool call re-reads only the factory's board of its work", async () => {
    const stale = await staleAfter(cache(), invalidationsFor(acme, event('tool_call')));

    expect(stale).toContainEqual(keys.board(acme, goal));
    expect(stale).toContainEqual(keys.item(acme, 'goal#12'));
    expect(stale).not.toContainEqual(keys.metrics(acme, goal, {}));
    expect(stale).not.toContainEqual(keys.touches(acme, goal, {}));
    expect(stale).not.toContainEqual(keys.items(acme, goal, { state: 'waiting_human' }));
  });

  test('marks the overview keys, which the stream batches separately', () => {
    const overview = invalidationsFor(acme, event('approved')).filter(isOverviewKey);
    expect(overview).toEqual([['org', acme, 'overview']]);
  });
});

describe('factoryInvalidations', () => {
  test("an andon pull re-reads the factory's work, counts and overviews, but no item and no prefix", async () => {
    const stale = await staleAfter(
      cache(),
      factoryInvalidations(acme, { org_id: acme, factory_id: goal, kind: 'andon_pulled', at: '2026-10-24T10:00:00Z' }),
    );

    expect(stale).toEqual(
      expect.arrayContaining([
        keys.board(acme, goal),
        keys.metrics(acme, goal, {}),
        keys.attention(acme),
        keys.inbox(acme),
        keys.overview(acme, '7d'),
      ]),
    );
    expect(stale).not.toContainEqual(keys.item(acme, 'goal#12'));
    expect(stale).not.toContainEqual(keys.board(acme, sre));
    expect(stale).not.toContainEqual([...keys.factory(acme, goal), 'draft']);
    expect(stale.filter((k) => k[1] === labs)).toEqual([]);
  });
});

describe('resyncInvalidations', () => {
  test('re-reads everything of the organization and nothing of another', async () => {
    const stale = await staleAfter(cache(), resyncInvalidations(acme));

    expect(stale).toHaveLength(15);
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
  client.setQueryData(keys.metrics(acme, goal, {}), {});
  client.setQueryData(defKeys.factory(acme, goal), {});
  client.setQueryData(defKeys.draft(acme, goal), {});
  client.setQueryData(defKeys.draftModel(acme, goal), {});
  client.setQueryData(defKeys.draftFile(acme, goal, 'factory.yaml'), {});
  client.setQueryData(defKeys.validation(acme, goal, 4), {});
  client.setQueryData(defKeys.versions(acme, goal), {});
  client.setQueryData(defKeys.version(acme, goal, '8'), {});
  client.setQueryData(defKeys.compare(acme, goal, 'base', 'draft', 42), {});
  client.setQueryData(defKeys.versionFile(acme, 'a1b2'), '');
  client.setQueryData(defKeys.templates(acme), []);
  client.setQueryData(defKeys.draft(acme, sre), {});
  return client;
}

const definition = (e: Partial<DefinitionEvent>): DefinitionEvent => ({
  org_id: acme,
  factory_id: goal,
  kind: 'draft',
  version_id: saved.draft,
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
        defKeys.compare(acme, goal, 'base', 'draft', 42),
        keys.board(acme, goal),
        keys.overview(acme, '7d'),
        keys.factories(acme),
      ]),
    );
    expect(stale).not.toContainEqual(keys.metrics(acme, goal, {}));
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

describe('the intake', () => {
  test('an item event and a publish of the running definition re-read the factory intake, and a draft save does not', () => {
    expect(invalidationsFor(acme, event('approved'))).toContainEqual(keys.intake(acme, goal));
    expect(definitionInvalidations(acme, definition({ kind: 'current', number: 9 }))).toContainEqual(
      keys.intake(acme, goal),
    );
    expect(definitionInvalidations(acme, definition({ kind: 'draft', number: 2 }))).not.toContainEqual(
      keys.intake(acme, goal),
    );
  });
});

describe('connectionInvalidations', () => {
  test('re-reads the lists, the connection, its activity and the kinds, never a definition or the work', async () => {
    const client = definitionCache();
    client.setQueryData(keys.connectionList(acme, {}), []);
    client.setQueryData(keys.connectionList(acme, { factory: goal }), []);
    client.setQueryData(keys.connection(acme, 'github'), {});
    client.setQueryData(keys.connectionActivity(acme), {});
    client.setQueryData(keys.connectionKinds(acme), []);
    client.setQueryData(keys.connectionList(labs, {}), []);

    const stale = await staleAfter(client, connectionInvalidations(acme));

    expect(stale).toEqual([
      keys.connectionList(acme, {}),
      keys.connectionList(acme, { factory: goal }),
      keys.connection(acme, 'github'),
      keys.connectionActivity(acme),
      // A connection of a kind another admin just defined: the kinds are re-read too.
      keys.connectionKinds(acme),
    ]);
  });

  test('a publish re-reads what uses each connection', () => {
    expect(definitionInvalidations(acme, definition({ kind: 'current', number: 9 }))).toContainEqual(
      keys.connections(acme),
    );
  });
});

describe('knowledgeInvalidations', () => {
  test("re-reads the organization's sources and their documents, never a search or anything else", async () => {
    const client = definitionCache();
    client.setQueryData(keys.knowledgeSources(acme, {}), []);
    client.setQueryData(keys.knowledgeSources(acme, { factory: goal }), []);
    client.setQueryData(keys.knowledgeSource(acme, 'handbook'), {});
    client.setQueryData([...keys.knowledgeDocuments(acme, 'handbook'), 100], {});
    client.setQueryData(keys.knowledgeDocument(acme, 'd1'), {});
    client.setQueryData(keys.knowledgeSearch(acme, { q: 'deploy' }), []);
    client.setQueryData(keys.connectionList(acme, {}), []);
    client.setQueryData(keys.knowledgeSources(labs, {}), []);

    const stale = await staleAfter(client, knowledgeInvalidations(acme));

    expect(stale).toEqual([
      keys.knowledgeSources(acme, {}),
      keys.knowledgeSources(acme, { factory: goal }),
      keys.knowledgeSource(acme, 'handbook'),
      [...keys.knowledgeDocuments(acme, 'handbook'), 100],
      keys.knowledgeDocument(acme, 'd1'),
    ]);
    // A search open on Knowledge is an embedding call on goald: it runs again when asked, not per sync.
  });

  test('a publish re-reads which factories may search each source', () => {
    expect(definitionInvalidations(acme, definition({ kind: 'current', number: 9 }))).toContainEqual(
      keys.knowledge(acme),
    );
  });
});

describe('a move in flight', () => {
  test("holds back the stream's refetch of its factory's board, which would put the card back", async () => {
    const client = new QueryClient();
    let finish = () => {};
    const answered = new Promise<void>((resolve) => {
      finish = resolve;
    });
    const move = client
      .getMutationCache()
      .build(client, { mutationKey: keys.move(acme, goal), mutationFn: () => answered })
      .execute(undefined);

    expect(heldByMove(client, keys.board(acme, goal))).toBe(true);
    expect(heldByMove(client, keys.board(acme, sre))).toBe(false);
    expect(heldByMove(client, keys.board(labs, goal))).toBe(false);
    expect(heldByMove(client, keys.metrics(acme, goal, {}))).toBe(false);

    finish();
    await move;
    expect(heldByMove(client, keys.board(acme, goal))).toBe(false);
  });
});
