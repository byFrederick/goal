import { QueryClient, queryOptions } from '@tanstack/react-query';
import { describe, expect, test } from 'vitest';
import { ensureFound } from './queries';

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
