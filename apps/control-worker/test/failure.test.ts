// What a public request gets when goald cannot answer it. The errors are the ones production logged
// during the deploy rollovers of 2026-11-02 (docs/engineering/cloudflare-operations.md).

import { createScheduledController, env, runDurableObjectAlarm, runInDurableObject } from 'cloudflare:test';
import { exports } from 'cloudflare:workers';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import worker from '../src/index.js';
import { type FakeContainer, fakeFor } from './fake.js';
import { Auth, Goald } from './worker.js';

const goald = () => fakeFor(env.GOALD.idFromName('goald'));
const goaldStub = () => env.GOALD.get(env.GOALD.idFromName('goald'));
const page = { headers: { accept: 'text/html,application/xhtml+xml;q=0.9,*/*;q=0.8', cookie: 'session=test-token' } };

/** failOnce makes the container's port throw error on its next fetch, then answer with ok. */
function failOnce(c: FakeContainer, error: Error): { calls: number } {
  const seen = { calls: 0 };
  c.port = async () => {
    seen.calls++;
    if (seen.calls === 1) throw error;
    return new Response('ok');
  };
  return seen;
}

/** retryable is an error as a Durable Object stub throws it after a deploy reset the object. */
function retryable(overloaded = false): Error {
  return Object.assign(new Error('Durable Object reset because its code was updated.'), {
    retryable: true,
    overloaded,
  });
}

let logged: ReturnType<typeof vi.spyOn>;

beforeEach(async () => {
  const c = goald();
  c.port = () => new Response('ok');
  if (!c.running) await worker.scheduled(createScheduledController(), env);
  logged = vi.spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(() => {
  logged.mockRestore();
});

describe('when goald throws', () => {
  it('answers a page load with the 502 page and logs one line without headers', async () => {
    goald().port = async () => {
      throw new Error('There is no container instance that can be provided to this Durable Object, try again later');
    };
    const res = await exports.default.fetch('https://goal.example.com/o/acme/needs-you?tab=all', page);

    expect(res.status).toBe(502);
    expect(res.headers.get('content-type')).toBe('text/html; charset=utf-8');
    expect(res.headers.get('cache-control')).toBe('no-store');
    const html = await res.text();
    expect(html).toContain('<h1>Goal is not answering</h1>');
    expect(html).toContain('<a class="primary" href="">Reload</a>');
    // One inline document: no script, nothing fetched from elsewhere.
    expect(html).not.toMatch(/<script|<link|src=|url\(|@import/i);

    expect(logged).toHaveBeenCalledTimes(1);
    const line = String(logged.mock.calls[0]?.[0]);
    expect(line).toContain('GET /o/acme/needs-you');
    expect(line).toContain('There is no container instance');
    expect(line).not.toContain('test-token');
    expect(line).not.toContain('tab=all');
  });

  it('answers an API call with a problem+json 502', async () => {
    goald().port = async () => {
      throw new Error('The container is not running, consider calling start()');
    };
    for (const path of ['/v1/orgs', '/api/auth/get-session']) {
      const res = await exports.default.fetch(`https://goal.example.com${path}`, page);
      expect(res.status).toBe(502);
      expect(res.headers.get('content-type')).toBe('application/problem+json');
      expect(await res.json()).toMatchObject({
        status: 502,
        title: 'Goal is not answering',
        code: 'goald-unavailable',
      });
    }
  });
});

describe('retries', () => {
  it('retries a GET once on a fresh stub when the error is retryable', async () => {
    const seen = failOnce(goald(), retryable());
    const res = await exports.default.fetch('https://goal.example.com/o/acme/f/software/board', page);
    expect(res.status).toBe(200);
    expect(await res.text()).toBe('ok');
    expect(seen.calls).toBe(2);
    expect(logged).not.toHaveBeenCalled();
  });

  it('never retries a POST', async () => {
    const seen = failOnce(goald(), retryable());
    const res = await exports.default.fetch('https://goal.example.com/v1/orgs', { method: 'POST', body: '{}' });
    expect(res.status).toBe(502);
    expect(seen.calls).toBe(1);
  });

  it('never retries an overloaded Durable Object', async () => {
    const seen = failOnce(goald(), retryable(true));
    const res = await exports.default.fetch('https://goal.example.com/o/acme/needs-you', page);
    expect(res.status).toBe(502);
    expect(seen.calls).toBe(1);
  });

  it('starts goald again when its container stopped under a GET', async () => {
    const c = goald();
    const seen = { calls: 0 };
    c.port = async () => {
      seen.calls++;
      if (seen.calls === 1) {
        await c.destroy();
        throw new Error('The container is not running, consider calling start()');
      }
      return new Response('ok');
    };
    c.started = [];
    const res = await exports.default.fetch('https://goal.example.com/o/acme/needs-you', page);
    expect(res.status).toBe(200);
    expect(c.started).toHaveLength(1);
  });
});

describe('booting goald', () => {
  it('starts a stopped goald once when the cron and a page load arrive together', async () => {
    const c = goald();
    await c.destroy();
    c.started = [];
    const [, res] = await Promise.all([
      worker.scheduled(createScheduledController(), env),
      exports.default.fetch('https://goal.example.com/o/acme/needs-you', page),
    ]);
    expect(res.status).toBe(200);
    expect(c.started).toHaveLength(1);
    expect(logged).not.toHaveBeenCalled();
  });

  it('tries the start again while Cloudflare has no container instance yet', async () => {
    const c = goald();
    await c.destroy();
    c.started = [];
    c.startFailures = [
      new Error('There is no container instance that can be provided to this Durable Object, try again later'),
    ];
    const res = await exports.default.fetch('https://goal.example.com/o/acme/needs-you', page);
    expect(res.status).toBe(200);
    expect(c.started).toHaveLength(1);
  });
});

/** lines are the JSON lines a console spy saw, parsed. */
function lines(spy: ReturnType<typeof vi.spyOn>): Record<string, unknown>[] {
  return spy.mock.calls.flatMap(([line]: unknown[]) => {
    try {
      return [JSON.parse(String(line)) as Record<string, unknown>];
    } catch {
      return [];
    }
  });
}

// A restart is a new instance of the class on the same Durable Object, its container still
// running: the tests build one inside the object, as Cloudflare does after it resets one.
describe('when Cloudflare restarts a Durable Object', () => {
  let info: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    info = vi.spyOn(console, 'log').mockImplementation(() => {});
  });

  afterEach(() => {
    info.mockRestore();
  });

  it('sets the inactivity timeout again when a restarted goald finds its container running', async () => {
    const cases = [
      { ns: env.GOALD, Service: Goald, service: 'goald', idleMs: 15 * 60_000 },
      { ns: env.AUTH, Service: Auth, service: 'auth', idleMs: 30 * 60_000 },
    ];
    for (const { ns, Service, service, idleMs } of cases) {
      const id = ns.idFromName(service);
      const stub = ns.get(id);
      const c = fakeFor(id);
      c.port = () => new Response('ok');
      await stub.ensure();
      c.idleMs = undefined;
      c.started = [];
      const fetched = { calls: 0 };
      c.port = () => {
        fetched.calls++;
        return new Response('ok');
      };
      const monitors = c.monitors;

      // Only the constructor runs: no forward(), no ensure().
      await runInDurableObject(stub, (_, state) => {
        new Service(state, env);
      });
      // blockConcurrencyWhile holds the next call until the timeout is set.
      await runInDurableObject(stub, () => {});

      expect(c.idleMs).toBe(idleMs);
      expect(c.started).toHaveLength(0);
      expect(fetched.calls).toBe(0);
      expect(c.monitors).toBe(monitors + 1);
      expect(lines(info)).toContainEqual({ msg: 'container found running', service });
    }
  });

  it('keeps an alarm set on goald, which a restarted Durable Object wakes to', async () => {
    await goaldStub().ensure();

    expect(await runDurableObjectAlarm(goaldStub())).toBe(true);

    const next = await runInDurableObject(goaldStub(), (_, state) => state.storage.getAlarm());
    expect(next).not.toBeNull();
  });

  it('answers within bootMs when a health check hangs', { timeout: 15_000 }, async () => {
    const c = goald();
    await goaldStub().ensure();
    const health = { calls: 0 };
    c.port = (_port, request) => {
      if (new URL(request.url).pathname !== '/v1/health') return new Response('page');
      health.calls++;
      // The first check never answers; the next one does.
      if (health.calls === 1) return new Promise<Response>(() => {});
      return new Response('ok');
    };

    // A restarted goald boots against its running container on its first call.
    const started = Date.now();
    await runInDurableObject(goaldStub(), (_, state) => new Goald(state, env).ensure());

    expect(health.calls).toBe(2);
    expect(Date.now() - started).toBeLessThan(10_000);
  });
});

describe('when a container stops', () => {
  let info: ReturnType<typeof vi.spyOn>;
  let warned: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    info = vi.spyOn(console, 'log').mockImplementation(() => {});
    warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    info.mockRestore();
    warned.mockRestore();
  });

  it('logs one line with the exit code and the reason', async () => {
    const cases: { stop: (c: FakeContainer) => void; exitCode: number | null; reason: string }[] = [
      { stop: (c) => c.exit(0), exitCode: 0, reason: 'exited' },
      { stop: (c) => c.exit(137), exitCode: 137, reason: 'exited with code 137' },
      { stop: (c) => c.exit(0, new Error('the host went away')), exitCode: null, reason: 'the host went away' },
    ];
    // An object of its own: the restart tests above left extra instances watching goald's container.
    const id = env.GOALD.idFromName('stops');
    const c = fakeFor(id);
    for (const { stop, exitCode, reason } of cases) {
      await env.GOALD.get(id).ensure();
      warned.mockClear();

      stop(c);

      await vi.waitFor(() => expect(lines(warned)).toHaveLength(1));
      expect(lines(warned)[0]).toEqual({ msg: 'container stopped', service: 'goald', exitCode, reason });
    }
  });

  it('logs a line when it starts a container, and none carries a setting', async () => {
    const c = goald();
    await c.destroy();
    await vi.waitFor(() => expect(warned).toHaveBeenCalled());

    await exports.default.fetch('https://goal.example.com/o/acme/needs-you?tab=all', page);

    expect(lines(info)).toContainEqual({ msg: 'container started', service: 'goald' });
    for (const spy of [info, warned]) {
      const text = JSON.stringify(spy.mock.calls);
      for (const secret of ['master-key-for-tests', 'test-token', 'app-password-for-tests', 'tab=all']) {
        expect(text).not.toContain(secret);
      }
    }
  });
});
