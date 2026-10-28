import { QueryClient, QueryObserver, queryOptions } from '@tanstack/react-query';
import { describe, expect, test, vi } from 'vitest';
import { defKeys, ensureFound, knowledgeDocumentUsesQuery, knowledgeUsageQuery, validationQuery } from './queries';
import type { Validation } from './types';

// goald validates the draft as it is when the request arrives, which may be a later revision than
// the one the editor asked about.
const goald = vi.hoisted(() => ({ revision: 42 }));
vi.mock(import('./definitions'), async (original) => ({
  ...(await original()),
  validateDraft: async () => ({ revision: goald.revision, ok: true, problems: [] }) as unknown as Validation,
}));

// A list cached for a minute (the organizations, a factory list) that goald has grown since.
function list(answers: string[][]) {
  let reads = 0;
  const options = queryOptions({
    queryKey: ['orgs'],
    queryFn: async () => answers[Math.min(reads++, answers.length - 1)] ?? [],
    staleTime: 60_000,
  });
  return { options, reads: () => reads };
}

const named = (name: string) => (names: string[]) => names.find((n) => n === name);

describe('ensureFound', () => {
  test('reads the list again once when the cached one lacks what the URL names', async () => {
    const client = new QueryClient();
    const orgs = list([['acme'], ['acme', 'new-co']]);
    await client.ensureQueryData(orgs.options);

    expect(await ensureFound(client, orgs.options, named('new-co'))).toBe('new-co');
    expect(orgs.reads()).toBe(2);
  });

  test('gives up after that one read', async () => {
    const client = new QueryClient();
    const orgs = list([['acme']]);

    expect(await ensureFound(client, orgs.options, named('gone'))).toBeUndefined();
    expect(orgs.reads()).toBe(2);
  });

  test('reads nothing again when the cached list has it', async () => {
    const client = new QueryClient();
    const orgs = list([['acme']]);
    await client.ensureQueryData(orgs.options);

    expect(await ensureFound(client, orgs.options, named('acme'))).toBe('acme');
    expect(orgs.reads()).toBe(1);
  });
});

describe('validationQuery', () => {
  const org = 'acme';
  const f = '6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60';

  test('a result for the revision asked about stays fresh', async () => {
    const client = new QueryClient();
    goald.revision = 42;
    await client.fetchQuery(validationQuery(org, f, 42));

    expect(new QueryObserver(client, validationQuery(org, f, 42)).getCurrentResult().isStale).toBe(false);
  });

  test('a result for a later revision is kept under that revision, not as the one asked about', async () => {
    const client = new QueryClient();
    goald.revision = 43;
    await client.fetchQuery(validationQuery(org, f, 42));

    expect(client.getQueryData<Validation>(defKeys.validation(org, f, 43))?.revision).toBe(43);
    // Read again when the editor next asks about revision 42 (it has moved on by then).
    expect(new QueryObserver(client, validationQuery(org, f, 42)).getCurrentResult().isStale).toBe(true);
  });
});

describe('knowledge usage keys', () => {
  test('the usage of a source and the uses of a document sit under the knowledge prefix the stream refreshes', () => {
    expect(knowledgeUsageQuery('org-1', 'goal-docs').queryKey).toEqual([
      'org',
      'org-1',
      'knowledge',
      'usage',
      'goal-docs',
      7,
    ]);
    expect(knowledgeDocumentUsesQuery('org-1', 'd1').queryKey).toEqual([
      'org',
      'org-1',
      'knowledge',
      'document',
      'd1',
      'uses',
    ]);
  });
});
