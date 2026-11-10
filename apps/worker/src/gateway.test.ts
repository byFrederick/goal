import { Readable } from 'node:stream';
import { describe, expect, it } from 'vitest';
import { gatewayFetch, maxTotalWaitMs, waitSeconds } from './gateway.js';

const url = 'http://user:secret@gateway.internal/run/build-cache/abc?sig=signed-credential';

/** The 503 goald and the control Worker answer while goald starts. */
function starting(retryAfter?: string): Response {
  return Response.json(
    { type: 'about:blank', title: 'Service Unavailable', status: 503, code: 'goald-starting' },
    { status: 503, headers: retryAfter === undefined ? {} : { 'Retry-After': retryAfter } },
  );
}

/** A fetch answering with answers in order, the last one repeated, and a fake clock that sleep moves. */
function fake(...answers: (() => Response)[]) {
  const calls: { input: string | URL | Request; init: RequestInit | undefined }[] = [];
  const sleeps: number[] = [];
  const logs: string[] = [];
  let clock = 1_000_000;
  const fetch = (async (input: string | URL | Request, init?: RequestInit) => {
    calls.push({ input, init });
    const answer = answers[Math.min(calls.length, answers.length) - 1];
    if (!answer) throw new Error('no answer');
    return answer();
  }) as typeof globalThis.fetch;
  const deps = {
    fetch,
    sleep: async (ms: number) => {
      sleeps.push(ms);
      clock += ms;
    },
    now: () => clock,
    log: (line: string) => logs.push(line),
  };
  return { deps, calls, sleeps, logs };
}

describe('gatewayFetch', () => {
  it('retries a 503 goald-starting after its Retry-After seconds and returns the next answer', async () => {
    const f = fake(
      () => starting('3'),
      () => Response.json({ ok: true }),
    );

    const res = await gatewayFetch(f.deps, url);

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ ok: true });
    expect(f.calls).toHaveLength(2);
    expect(f.sleeps).toEqual([3_000]);
    expect(f.logs).toEqual([
      'goal-agent: gateway /run/build-cache/abc is starting (503 goald-starting); attempt 1 failed, retrying in 3s',
    ]);
    expect(f.logs.join('\n')).not.toMatch(/secret|signed-credential|user/);
  });

  it('waits the fallback without a Retry-After, and clamps one out of bounds', async () => {
    const f = fake(
      () => starting(),
      () => starting('not-a-number'),
      () => starting('600'),
      () => starting('0'),
      () => new Response('ok'),
    );

    expect((await gatewayFetch(f.deps, url)).status).toBe(200);
    expect(f.sleeps).toEqual([5_000, 5_000, 30_000, 1_000]);
    expect(waitSeconds('2.5')).toBe(3);
  });

  it('stops at the total limit and returns the last 503', async () => {
    const f = fake(() => starting('30'));

    const res = await gatewayFetch(f.deps, url);

    expect(res.status).toBe(503);
    expect(await res.json()).toMatchObject({ code: 'goald-starting' });
    expect(f.sleeps).toEqual([30_000, 30_000, 30_000, 30_000]);
    expect(f.sleeps.reduce((a, b) => a + b, 0)).toBeLessThanOrEqual(maxTotalWaitMs);
    expect(f.calls).toHaveLength(5);
  });

  it('stops at the limit even when the clock stands still', async () => {
    const f = fake(() => starting('30'));
    const res = await gatewayFetch({ ...f.deps, now: () => 0 }, url);

    expect(res.status).toBe(503);
    expect(f.calls).toHaveLength(5);
  });

  it.each([
    ['a plain 503', () => new Response(null, { status: 503 })],
    ['a 503 with another code', () => Response.json({ code: 'database-down' }, { status: 503 })],
    ['a 503 whose body is not JSON', () => new Response('unavailable', { status: 503 })],
    ['a 500', () => Response.json({ code: 'goald-starting' }, { status: 500 })],
    ['a 200', () => new Response('ok')],
  ])('returns %s at once, without a wait or a log', async (_, answer) => {
    const f = fake(answer, () => new Response('second'));

    const res = await gatewayFetch(f.deps, url);

    expect(await res.text()).not.toBe('second');
    expect(f.calls).toHaveLength(1);
    expect(f.sleeps).toEqual([]);
    expect(f.logs).toEqual([]);
  });

  it('retries a string body, sending it again', async () => {
    const f = fake(
      () => starting('1'),
      () => new Response(null, { status: 202 }),
    );

    const res = await gatewayFetch(f.deps, url, { method: 'POST', body: '{"a":1}' });

    expect(res.status).toBe(202);
    expect(f.calls.map((c) => c.init?.body)).toEqual(['{"a":1}', '{"a":1}']);
  });

  it('never retries a request whose body is a stream', async () => {
    const f = fake(
      () => starting('1'),
      () => new Response('second'),
    );
    const body = Readable.toWeb(Readable.from([Buffer.from('archive')])) as unknown as RequestInit['body'];

    const res = await gatewayFetch(f.deps, url, { method: 'PUT', body, duplex: 'half' } as RequestInit);

    expect(res.status).toBe(503);
    expect(f.calls).toHaveLength(1);
    expect(f.sleeps).toEqual([]);
  });

  it('ends the wait when the signal aborts, rejecting with its reason', async () => {
    const abort = new AbortController();
    const f = fake(() => starting('10'));
    const deps = { ...f.deps, sleep: () => new Promise<void>(() => abort.abort(new Error('restore timed out'))) };

    await expect(gatewayFetch(deps, url, { signal: abort.signal })).rejects.toThrow('restore timed out');
    expect(f.calls).toHaveLength(1);
  });

  it('gives each attempt its own time limit, joined to the caller signal', async () => {
    const f = fake(
      () => starting('1'),
      () => new Response('ok'),
    );

    await gatewayFetch(f.deps, url, { method: 'POST', body: 'x' }, { attemptTimeoutMs: 30_000 });

    const [first, second] = f.calls.map((c) => c.init?.signal);
    expect(first).toBeInstanceOf(AbortSignal);
    expect(second).toBeInstanceOf(AbortSignal);
    expect(first).not.toBe(second);
  });
});
