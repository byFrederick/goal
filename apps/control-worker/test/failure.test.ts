// What a public request gets when goald cannot answer it. The errors are the ones production logged
// during the deploy rollovers of 2026-11-02 (docs/engineering/cloudflare-operations.md).

import { createScheduledController, env } from 'cloudflare:test';
import { exports } from 'cloudflare:workers';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import worker from '../src/index.js';
import { type FakeContainer, fakeFor } from './fake.js';

const goald = () => fakeFor(env.GOALD.idFromName('goald'));
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
