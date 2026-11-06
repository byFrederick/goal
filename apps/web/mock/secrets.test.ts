// The mock's secrets API: the list, a create, an import (a dry run and a real one), a replaced value,
// settings, a delete, and the audit entries about a secret. The answers are read as the pages read
// them, and no value is ever answered or kept.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Actor, Factory, Org } from '@goal/api-client';
import { describe, expect, it } from 'vitest';
import { createSecrets } from './secrets.ts';

const org = { id: 'org-1', slug: 'acme', name: 'Acme' } as unknown as Org;
const by: Actor = { kind: 'user', id: 'local', name: 'Local user' };
const factories = [
  { id: 'f-software', name: 'software' },
  { id: 'f-sre', name: 'sre' },
] as unknown as Factory[];

/** A mock with the seeded secrets, and a call to its handler that returns the status and the JSON answer. */
function mock() {
  const secrets = createSecrets({
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
    const [path = '', query = ''] = rest.split('?');
    const url = new URL(`http://mock/v1/orgs/acme${path}?${query}`);
    await secrets.handle(org, path, method, url, {} as IncomingMessage, res, async () => body);
    return { status, answer: answer as Record<string, unknown> };
  };
  return { call };
}

const sample = 'secrets:\n  sentry-token: sntrys_example\n  linear-key: lin_example\n  github-token: gh_example\n';

describe('the mock repository test', () => {
  const test = (url: string, secret = 'github-token', access = 'write') =>
    mock().call('/factories/f-sre/repositories/test', 'POST', { url, secret, access });

  it('answers read and push ok for a repository the token reaches', async () => {
    const { status, answer } = await test('https://github.com/acme/app');
    expect(status).toBe(200);
    expect(answer).toMatchObject({ tested: true, ok: true, read: { ok: true }, push: { ok: true } });
  });

  it('answers a failing push with the host’s reason, and refuses an unknown secret or address', async () => {
    const { answer } = await test('https://github.com/acme/app-readonly');
    expect(answer).toMatchObject({ ok: false, read: { ok: true }, push: { ok: false } });
    expect((await test('https://github.com/acme/app', 'nope-token')).status).toBe(404);
    expect((await test('https://example.com/acme/app')).status).toBe(400);
  });
});

describe('the mock secrets', () => {
  it('lists the eleven secrets (the nine of the picture, slack-bot and confluence-token), with no value', async () => {
    const { answer } = await mock().call('/secrets', 'GET');
    const list = answer.secrets as { name: string; uses: unknown[] }[];

    expect(list).toHaveLength(11);
    expect(list.find((s) => s.name === 'github-token')?.uses).toHaveLength(5);
    expect(JSON.stringify(answer)).not.toContain('"value":');
  });

  it('refuses a bad name with secret-invalid and a taken one with secret-name-taken', async () => {
    const { call } = mock();
    const bad = await call('/secrets', 'POST', { name: 'Bad Name', value: 'x' });
    const taken = await call('/secrets', 'POST', { name: 'claude', value: 'x' });

    expect(bad).toMatchObject({ status: 400, answer: { code: 'secret-invalid' } });
    expect(taken).toMatchObject({ status: 409, answer: { code: 'secret-name-taken' } });
  });

  it('creates a secret open to the factories chosen, and records it as created', async () => {
    const { call } = mock();
    const made = await call('/secrets', 'POST', {
      name: 'new-key',
      value: 'v',
      scope: { all_factories: false, factories: ['f-sre'] },
    });
    const secret = made.answer as { id: string; scope: unknown };
    const activity = await call(`/audit?target_kind=secret&target_id=${secret.id}`, 'GET');

    expect(made.status).toBe(201);
    expect(secret.scope).toEqual({ all_factories: false, factories: ['f-sre'] });
    expect((activity.answer.entries as { action: string }[]).map((e) => e.action)).toEqual(['secret.created']);
  });

  it('dry-runs an import as names with their uses, and writes the new ones on a real run', async () => {
    const { call } = mock();
    const dry = await call('/secrets/import', 'POST', { format: 'yaml', text: sample, dry_run: true });
    const real = await call('/secrets/import', 'POST', {
      format: 'yaml',
      text: sample,
      on_conflict: 'skip',
      dry_run: false,
    });

    expect(dry.answer).toMatchObject({
      dry_run: true,
      secrets: [
        { name: 'sentry-token', exists: true },
        { name: 'linear-key', exists: true },
        { name: 'github-token', exists: true },
      ],
    });
    expect((real.answer.secrets as { outcome: string }[]).map((s) => s.outcome)).toEqual([
      'skipped',
      'skipped',
      'skipped',
    ]);
  });

  it('replaces a value without answering it, and shows the replace in the secret’s activity', async () => {
    const { call } = mock();
    const put = await call('/secrets/claude/value', 'PUT', { value: 'new' });
    const { answer } = await call('/audit?target_kind=secret&target_id=secret-claude', 'GET');

    expect(put.status).toBe(200);
    expect(JSON.stringify(put.answer)).not.toContain('"new"');
    expect((answer.entries as { action: string }[])[0]?.action).toBe('secret.value_replaced');
  });

  it('changes the description, scope and expiry through a patch, and refuses a patch that changes nothing', async () => {
    const { call } = mock();
    const patched = await call('/secrets/claude', 'PATCH', {
      description: 'Shared',
      expiry: { expires_at: '2027-01-01T00:00:00Z', remind_days: 7 },
    });
    const empty = await call('/secrets/claude', 'PATCH', {});

    expect(patched.answer).toMatchObject({ description: 'Shared', remind_days: 7 });
    expect(empty).toMatchObject({ status: 400, answer: { code: 'secret-invalid' } });
  });

  it('refuses to delete a secret in use, naming the uses, and deletes one nothing uses', async () => {
    const { call } = mock();
    const refused = await call('/secrets/claude', 'DELETE');
    const deleted = await call('/secrets/old-npm-token', 'DELETE');
    const gone = await call('/secrets/old-npm-token', 'GET');

    expect(refused).toMatchObject({ status: 409, answer: { code: 'secret-in-use' } });
    expect(deleted.status).toBe(204);
    expect(gone).toMatchObject({ status: 404, answer: { code: 'secret-not-found' } });
  });
});
