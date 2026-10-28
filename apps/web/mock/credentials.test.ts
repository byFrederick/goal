// The mock's credentials API for the New credential screen: the create and the import (a dry run and
// a real one). The answers are read as the page reads them, and no value is ever answered or kept.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Actor, Factory, Org } from '@goal/api-client';
import { describe, expect, it } from 'vitest';
import { createCredentials } from './credentials.ts';

const org = { id: 'org-1', slug: 'acme', name: 'Acme' } as unknown as Org;
const by: Actor = { kind: 'user', id: 'local', name: 'Local user' };
const factories = [
  { id: 'f-software', name: 'software' },
  { id: 'f-sre', name: 'sre' },
] as unknown as Factory[];

/** A mock with the seeded credentials, and a call to its handler that returns the status and the JSON answer. */
function mock() {
  const credentials = createCredentials({
    clock: () => Date.parse('2026-10-27T12:00:00Z'),
    actor: () => by,
    stale: () => false,
    factories: () => factories,
    acme: 'org-1',
  });
  const call = async (rest: string, method: string, body: Record<string, unknown> = {}) => {
    let status = 0;
    let answer: unknown;
    const res = {
      setHeader() {},
      end(chunk?: string) {
        if (chunk) answer = JSON.parse(chunk);
      },
      set statusCode(code: number) {
        status = code;
      },
    } as unknown as ServerResponse;
    await credentials.handle(org, rest, method, {} as IncomingMessage, res, async () => body);
    return { status, answer: answer as Record<string, unknown> };
  };
  return { call };
}

const sample =
  'credentials:\n  sentry-token: sntrys_example\n  linear-key: lin_example\n  cloudflare-token: cf_example\n';

describe('the mock credentials import', () => {
  it('dry-runs the picture’s sample as three names, two new and one existing with two connections', async () => {
    const { call } = mock();
    const { status, answer } = await call('/credentials/import', 'POST', {
      format: 'yaml',
      text: sample,
      dry_run: true,
    });

    expect(status).toBe(200);
    expect(answer).toMatchObject({
      dry_run: true,
      credentials: [
        { name: 'sentry-token', exists: false, connections: 0 },
        { name: 'linear-key', exists: false, connections: 0 },
        { name: 'cloudflare-token', exists: true, connections: 2 },
      ],
    });
  });

  it('writes the new names with no factories, and skips the existing one', async () => {
    const { call } = mock();
    const { answer } = await call('/credentials/import', 'POST', {
      format: 'yaml',
      text: sample,
      on_conflict: 'skip',
      dry_run: false,
    });

    expect(answer.credentials).toEqual([
      { name: 'sentry-token', exists: false, connections: 0, outcome: 'created' },
      { name: 'linear-key', exists: false, connections: 0, outcome: 'created' },
      { name: 'cloudflare-token', exists: true, connections: 2, outcome: 'skipped' },
    ]);
    const list = await call('/credentials', 'GET');
    const sentry = (
      list.answer.credentials as { name: string; scope: { all_factories: boolean; factories: string[] } }[]
    ).find((c) => c.name === 'sentry-token');
    expect(sentry?.scope).toEqual({ all_factories: false, factories: [] });
  });

  it('refuses a text that does not parse, and a name that is not a credential name', async () => {
    const { call } = mock();
    expect(
      (await call('/credentials/import', 'POST', { format: 'yaml', text: 'no map here', dry_run: true })).status,
    ).toBe(400);
    expect(
      (await call('/credentials/import', 'POST', { format: 'env', text: 'AB=x', dry_run: true })).status,
    ).toBe(400);
  });
});

describe('the mock credential create', () => {
  const body = {
    name: 'e2e-token-2028',
    type: 'api_key',
    value: 'test-value',
    description: '',
    scope: { all_factories: false, factories: ['f-software'] },
    agents: { 'f-software': ['architect'] },
    remind_days: 0,
  };

  it('creates the credential with its scope by factory id and answers it without the value', async () => {
    const { call } = mock();
    const { status, answer } = await call('/credentials', 'POST', body);

    expect(status).toBe(201);
    expect(answer).toMatchObject({
      name: 'e2e-token-2028',
      scope: { all_factories: false, factories: ['f-software'] },
      agents: { 'f-software': ['architect'] },
    });
    expect(JSON.stringify(answer)).not.toContain('test-value');
  });

  it('refuses an invalid name with 400 and a taken name with 409', async () => {
    const { call } = mock();
    expect((await call('/credentials', 'POST', { ...body, name: 'Bad_Name' })).status).toBe(400);
    const taken = await call('/credentials', 'POST', { ...body, name: 'cloudflare-token' });
    expect(taken.status).toBe(409);
    expect(taken.answer.code).toBe('credential-name-taken');
  });
});
