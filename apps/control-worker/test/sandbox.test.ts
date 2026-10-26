import { env, runDurableObjectAlarm } from 'cloudflare:test';
import { exports } from 'cloudflare:workers';
import { describe, expect, it } from 'vitest';
import { fakeFor, imageBase } from './fake.js';

const key = 'sandbox-key-for-tests-0123456789abcdef';
const api = 'http://sandbox.internal/v1/sandboxes';

/** call sends a request to the sandbox API the way goald's container does: through its outbound handler. */
function call(method: string, path: string, body?: unknown, bearer = key): Promise<Response> {
  const init: RequestInit = { method, headers: { authorization: `Bearer ${bearer}` } };
  if (body !== undefined) init.body = JSON.stringify(body);
  return exports.GoaldOutbound.fetch(`${api}${path}`, init);
}

let runs = 0;

function newRun(): string {
  runs++;
  return `019a4000-0000-7000-8000-${String(runs).padStart(12, '0')}`;
}

function spec(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    org: 'org-acme',
    factory: 'factory-1',
    item: 'item-1',
    station: 'build',
    agent: 'builder',
    image: 'goal-agent',
    size: 'small',
    env: { GOAL_GATEWAY_URL: 'http://gateway.internal/mcp' },
    deadline: new Date(Date.now() + 3_600_000).toISOString(),
    ...over,
  };
}

function box(run: string) {
  return fakeFor(env.BOXES.idFromName(run));
}

async function create(run: string, over: Record<string, unknown> = {}): Promise<Response> {
  const res = await call('PUT', `/${run}`, spec(over));
  expect(res.status).toBe(201);
  return res;
}

let execs = 0;

function execId(): string {
  execs++;
  return execs.toString(16).padStart(8, '0');
}

/** lines reads an NDJSON body one value at a time. */
function lines(res: Response): () => Promise<Record<string, unknown> | undefined> {
  if (!res.body) throw new Error(`HTTP ${res.status} without a body`);
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
  let buffered = '';
  return async () => {
    for (;;) {
      const newline = buffered.indexOf('\n');
      if (newline >= 0) {
        const line = buffered.slice(0, newline);
        buffered = buffered.slice(newline + 1);
        return JSON.parse(line) as Record<string, unknown>;
      }
      const { done, value } = await reader.read();
      if (done) return undefined;
      buffered += value;
    }
  };
}

async function all(res: Response): Promise<Record<string, unknown>[]> {
  const next = lines(res);
  const out: Record<string, unknown>[] = [];
  for (let v = await next(); v !== undefined; v = await next()) out.push(v);
  return out;
}

/** leaveAfterFirstLine reads an NDJSON body up to its first line, then goes away as a cut would. */
async function leaveAfterFirstLine(res: Response): Promise<Record<string, unknown>> {
  if (!res.body) throw new Error(`HTTP ${res.status} without a body`);
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
  let buffered = '';
  while (!buffered.includes('\n')) {
    const { done, value } = await reader.read();
    if (done) throw new Error('the stream ended before its first line');
    buffered += value;
  }
  await reader.cancel();
  return JSON.parse(buffered.slice(0, buffered.indexOf('\n'))) as Record<string, unknown>;
}

/** soon is what pending gives within ms, or 'nothing yet' (pending stays the stream's next line). */
function soon<T>(pending: Promise<T>, ms = 50): Promise<T | 'nothing yet'> {
  return Promise.race([pending, new Promise<'nothing yet'>((resolve) => setTimeout(() => resolve('nothing yet'), ms))]);
}

describe('the sandbox API', () => {
  it('refuses a request without the sandbox key', async () => {
    expect((await call('GET', '', undefined, 'wrong')).status).toBe(401);
    const res = await exports.GoaldOutbound.fetch(api);
    expect(res.status).toBe(401);
  });

  it('starts one box per run, from a named image at its size, and a second create returns it', async () => {
    const run = newRun();
    await create(run);

    const again = await call('PUT', `/${run}`, spec({ env: { OTHER: 'x' } }));
    expect(again.status).toBe(200);

    const c = box(run);
    expect(c.started).toHaveLength(1);
    expect(c.started[0]).toMatchObject({
      image: 'registry.example.com/goal-agent@sha256:0',
      instance: 'standard-2',
      enableInternet: true,
      labels: { run, org: 'org-acme' },
    });
    expect(c.started[0]?.entrypoint?.slice(0, 2)).toEqual(['/bin/sh', '-c']);
    expect(c.intercepts).toEqual(['gateway.internal']);
    expect(Number(c.idleMs)).toBeGreaterThan(3_500_000);

    const res = await call('POST', `/${run}/execs`, { id: execId(), argv: ['env'] });
    const [env1] = await all(res);
    expect(JSON.parse(String(env1?.o))).toMatchObject({ GOAL_GATEWAY_URL: 'http://gateway.internal/mcp' });
    expect(JSON.parse(String(env1?.o))).not.toHaveProperty('OTHER');

    const list = (await (await call('GET', '')).json()) as { sandboxes: { run: string; org: string }[] };
    expect(list.sandboxes.filter((s) => s.run === run)).toEqual([expect.objectContaining({ run, org: 'org-acme' })]);
  });

  it('maps each size to its instance type', async () => {
    for (const [size, instance] of [
      ['medium', 'standard-3'],
      ['large', 'standard-4'],
    ]) {
      const run = newRun();
      await create(run, { size });
      expect(box(run).started[0]?.instance).toBe(instance);
    }
  });

  it.each([
    ['a size it does not have', { size: 'huge' }],
    ['a size named after an object property', { size: 'toString' }],
    ['an image the Worker lacks', { image: 'debian:13' }],
    ['an image named after an object property', { image: 'constructor' }],
    ['a deadline in the past', { deadline: new Date(Date.now() - 1000).toISOString() }],
    ['a variable name that is not one', { env: { 'A=B': 'x' } }],
    ['no organization', { org: '' }],
  ])('refuses %s', async (_, over) => {
    const run = newRun();
    const res = await call('PUT', `/${run}`, spec(over));
    expect(res.status).toBe(400);
    expect(box(run).started).toHaveLength(0);
  });

  // Threat row 10 (spec 4 §11.2): the master key on Cloudflare never reaches a box.
  it('box start carries only goald’s env', async () => {
    const run = newRun();
    await create(run, { env: { GOAL_GATEWAY_URL: 'http://gateway.internal/mcp', FROM_GOALD: 'box' } });

    const res = await call('POST', `/${run}/execs`, { id: execId(), argv: ['env'], env: { FROM_EXEC: 'exec' } });
    const [out] = await all(res);
    expect(JSON.parse(String(out?.o))).toEqual({
      HOME: '/home/node',
      PATH: '/usr/bin:/bin',
      IMAGE_ONLY: 'image',
      GOAL_GATEWAY_URL: 'http://gateway.internal/mcp',
      FROM_GOALD: 'box',
      FROM_EXEC: 'exec',
    });

    const c = box(run);
    expect(c.started[0]).not.toHaveProperty('env');
    const seen = JSON.stringify([c.started, c.calls]);
    for (const [name, value] of Object.entries(env)) {
      if (typeof value === 'string' && !name.startsWith('__'))
        expect(seen, `the Worker's ${name}`).not.toContain(value);
    }
  });

  it('streams output and ends with the exit code and the stderr tail', async () => {
    const run = newRun();
    await create(run);

    expect(await all(await call('POST', `/${run}/execs`, { id: execId(), argv: ['echo', 'hi'] }))).toEqual([
      { o: 'hi\n' },
      { exit: 0, timed_out: false, stderr: '' },
    ]);
    expect(await all(await call('POST', `/${run}/execs`, { id: execId(), argv: ['fail', '3'] }))).toEqual([
      { o: 'out\n' },
      { exit: 3, timed_out: false, stderr: 'oops\n' },
    ]);
  });

  it('runs a command under timeout, in its dir or the image’s, with the image’s env under its own', async () => {
    const run = newRun();
    await create(run);
    await all(await call('POST', `/${run}/execs`, { id: execId(), argv: ['echo', 'a'], timeout_s: 30 }));
    await all(
      await call('POST', `/${run}/execs`, { id: execId(), argv: ['echo', 'b'], dir: '/tmp', env: { HOME: '/root' } }),
    );

    const [, first, second] = box(run).calls; // the first call reads the base file
    expect(first).toMatchObject({ argv: ['echo', 'a'], cwd: imageBase.split('\n')[0] });
    expect(second).toMatchObject({ argv: ['echo', 'b'], cwd: '/tmp', env: { HOME: '/root' } });
  });

  it('reports a command its timeout ended as timed out', async () => {
    const run = newRun();
    await create(run);
    const out = await all(await call('POST', `/${run}/execs`, { id: execId(), argv: ['long'], timeout_s: 1 }));
    expect(out).toEqual([{ o: 'first\n' }, { exit: 124, timed_out: true, stderr: '' }]);
  });

  it('kills a command when its client asks', async () => {
    const run = newRun();
    await create(run);
    const id = execId();
    const next = lines(await call('POST', `/${run}/execs`, { id, argv: ['long'] }));
    expect(await next()).toEqual({ o: 'first\n' });

    expect((await call('DELETE', `/${run}/execs/${id}`)).status).toBe(204);
    expect(await next()).toEqual({ exit: 143, timed_out: false, stderr: '' });
    expect(await next()).toBeUndefined();

    expect((await call('DELETE', `/${run}/execs/${id}`)).status).toBe(204); // ended: not an error
  });

  it('keeps a command running when its reader leaves, and an attach from an offset streams the rest and the outcome', async () => {
    const run = newRun();
    await create(run);
    const id = execId();
    expect(await leaveAfterFirstLine(await call('POST', `/${run}/execs`, { id, argv: ['long'] }))).toEqual({
      o: 'first\n',
    });

    // From the start again: the output is replayed, and the command still runs.
    const replay = lines(await call('GET', `/${run}/execs/${id}?offset=0`));
    expect(await replay()).toEqual({ o: 'first\n' });
    expect(await soon(replay())).toBe('nothing yet');

    // From where the reader got to: nothing twice, then the outcome once it ends.
    const rest = lines(await call('GET', `/${run}/execs/${id}?offset=6`));
    const outcome = rest();
    expect(await soon(outcome)).toBe('nothing yet');
    expect((await call('DELETE', `/${run}/execs/${id}`)).status).toBe(204);
    expect(await outcome).toEqual({ exit: 143, timed_out: false, stderr: '' });
    expect(await rest()).toBeUndefined();

    // Ended: an attach still gets the outcome (a cut may have lost it), with nothing twice.
    expect(await all(await call('GET', `/${run}/execs/${id}?offset=6`))).toEqual([
      { exit: 143, timed_out: false, stderr: '' },
    ]);
    expect((await call('GET', `/${run}/execs/${execId()}?offset=0`)).status).toBe(404);
    expect((await call('GET', `/${run}/execs/${id}?offset=-1`)).status).toBe(400);
  });

  it('ends running commands on delete, forgets the box and deletes idempotently', async () => {
    const run = newRun();
    await create(run);
    const next = lines(await call('POST', `/${run}/execs`, { id: execId(), argv: ['long'] }));
    expect(await next()).toEqual({ o: 'first\n' });

    expect((await call('DELETE', `/${run}`)).status).toBe(204);
    expect(await next()).toEqual({ error: expect.stringContaining('outcome is unknown') });
    expect(box(run).destroyed).toBe(1);

    const list = (await (await call('GET', '')).json()) as { sandboxes: { run: string }[] };
    expect(list.sandboxes.map((s) => s.run)).not.toContain(run);
    expect((await call('DELETE', `/${run}`)).status).toBe(204);
    expect((await call('DELETE', `/${newRun()}`)).status).toBe(204);
    expect((await call('POST', `/${run}/execs`, { id: execId(), argv: ['echo'] })).status).toBe(404);
  });

  it('destroys the box at its deadline even when nobody deletes it', async () => {
    const run = newRun();
    await create(run);

    expect(await runDurableObjectAlarm(env.BOXES.get(env.BOXES.idFromName(run)))).toBe(true);
    expect(box(run).destroyed).toBe(1);
    const list = (await (await call('GET', '')).json()) as { sandboxes: { run: string }[] };
    expect(list.sandboxes.map((s) => s.run)).not.toContain(run);
  });
});
