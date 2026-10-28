// The mock's policy answers (policy.ts): the acme policy as models.jpg draws it, the revision that a PUT
// bumps, the If-Match rules (428 without it, 409 when stale) and the 422 for a value goald refuses.
import type { IncomingMessage, ServerResponse } from 'node:http';
import { describe, expect, it } from 'vitest';
import { createPolicy, type PolicyFactory } from './policy.ts';

const factories: Record<string, PolicyFactory[]> = {
  org: [
    { id: 'f-software', name: 'software' },
    { id: 'f-sre', name: 'sre' },
    { id: 'f-support', name: 'support' },
  ],
};

/** A ServerResponse that keeps the status, headers and body the handler wrote. */
function response() {
  const out = { status: 0, headers: {} as Record<string, string>, body: undefined as unknown };
  const res = {
    setHeader: (name: string, value: string) => {
      out.headers[name] = value;
    },
    end: (body: string) => {
      out.body = JSON.parse(body);
    },
  };
  Object.defineProperty(res, 'statusCode', {
    get: () => out.status,
    set: (v: number) => {
      out.status = v;
    },
  });
  return { res: res as unknown as ServerResponse, out };
}

const policy = createPolicy({ clock: () => Date.UTC(2026, 9, 27, 9, 0), factoriesOf: (org) => factories[org] ?? [] });
const org = { id: 'org' };

function request(ifMatch?: string): IncomingMessage {
  return { headers: ifMatch === undefined ? {} : { 'if-match': ifMatch } } as unknown as IncomingMessage;
}

async function get() {
  const { res, out } = response();
  await policy.handle(org, '/policy', 'GET', request(), res, async () => ({}));
  return out;
}

/** A PUT of the given body with the If-Match it carries, answered by the mock. */
async function put(body: Record<string, unknown>, ifMatch?: string) {
  const { res, out } = response();
  await policy.handle(org, '/policy', 'PUT', request(ifMatch), res, async () => body);
  return out;
}

describe('the mock policy answers', () => {
  it('answers the acme policy as the picture draws it, at its revision and ETag', async () => {
    const out = await get();
    const view = out.body as {
      policy: { default_model: string; box_time_limit_minutes: number; tools: Record<string, string> };
      revision: number;
      models: { id: string; used_by: { name: string; agents: string[] }[] }[];
      box_sizes: { size: string; factories: { name: string }[] }[];
      egress_addresses: string[];
    };

    expect(out.status).toBe(200);
    expect(out.headers.ETag).toBe('"3"');
    expect(view.revision).toBe(3);
    expect(view.policy.default_model).toBe('claude-opus-5-5');
    expect(view.policy.box_time_limit_minutes).toBe(180);
    expect(view.policy.tools).toEqual({
      write_outside_branch: 'ask',
      merge_pull_request: 'review_approval',
      read_web: 'allowed',
      use_credentials: 'attached_only',
    });
    expect(view.models.map((m) => m.id)).toEqual([
      'claude-opus-5-5',
      'claude-sonnet-5-5',
      'claude-haiku-5-5',
      'claude-fable-5-1',
    ]);
    expect(view.models.find((m) => m.id === 'claude-sonnet-5-5')?.used_by).toEqual([
      { factory_id: 'f-support', name: 'support', agents: ['responder'] },
    ]);
    expect(view.box_sizes.find((b) => b.size === 'large')?.factories.map((f) => f.name)).toEqual(['software']);
    expect(view.egress_addresses).toEqual(['192.0.2.0/24', '2001:db8::/32']);
  });

  it('refuses a PUT without If-Match with 428, and a stale one with 409 that changes nothing', async () => {
    const current = (await get()).body as { policy: Record<string, unknown> };

    expect((await put(current.policy)).status).toBe(428);
    const stale = await put(current.policy, '"2"');
    expect(stale.status).toBe(409);
    expect((stale.body as { code: string }).code).toBe('policy-conflict');
    expect(((await get()).body as { revision: number }).revision).toBe(3);
  });

  it('saves a PUT on the current revision, bumps the revision and answers the new ETag', async () => {
    const current = (await get()).body as { policy: Record<string, unknown> };
    const saved = await put({ ...current.policy, box_time_limit_minutes: 90 }, '"3"');

    expect(saved.status).toBe(200);
    expect(saved.headers.ETag).toBe('"4"');
    expect((saved.body as { policy: { box_time_limit_minutes: number } }).policy.box_time_limit_minutes).toBe(90);
  });

  it('refuses a value goald refuses with 422 policy-invalid', async () => {
    const current = (await get()).body as { policy: Record<string, unknown> };
    const blank = await put({ ...current.policy, denied_commands: [' '] }, '"4"');

    expect(blank.status).toBe(422);
    expect((blank.body as { code: string }).code).toBe('policy-invalid');
  });
});
