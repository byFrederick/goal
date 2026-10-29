import { createScheduledController, env } from 'cloudflare:test';
import { exports } from 'cloudflare:workers';
import { describe, expect, it } from 'vitest';
import { fakeFor } from './fake.js';

const goald = () => fakeFor(env.GOALD.idFromName('goald'));
const auth = () => fakeFor(env.AUTH.idFromName('auth'));

/** echo makes a stand-in container answer with the port and what it was sent. */
function echo(c: ReturnType<typeof fakeFor>): void {
  c.port = async (port, request) =>
    Response.json({
      port,
      host: new URL(request.url).host,
      path: new URL(request.url).pathname,
      forwardedFor: request.headers.get('x-forwarded-for'),
    });
}

describe('routing', () => {
  it('sends every public request to goald, naming the client as Cloudflare saw it', async () => {
    echo(goald());
    const res = await exports.default.fetch('https://goal.example.com/v1/orgs', {
      headers: { 'cf-connecting-ip': '203.0.113.7', 'x-forwarded-for': '10.0.0.1' },
    });
    expect(await res.json()).toEqual({
      port: 8080,
      host: 'goal.example.com',
      path: '/v1/orgs',
      forwardedFor: '203.0.113.7',
    });
  });

  it('starts goald with its own settings and the topology, nothing else of the Worker', async () => {
    echo(goald());
    await exports.default.fetch('https://goal.example.com/');

    const started = goald().started[0]?.env ?? {};
    expect(started).toMatchObject({
      GOAL_MASTER_KEYS: 'k1:master-key-for-tests',
      GOAL_DATABASE_URL: expect.stringContaining('goal_app'),
      GOAL_SANDBOX_KEY: 'sandbox-key-for-tests-0123456789abcdef',
      GOAL_LISTEN: '0.0.0.0:8080',
      GOAL_GATEWAY_LISTEN: '0.0.0.0:8090',
      GOAL_GATEWAY_URL: 'http://gateway.internal/mcp',
      GOAL_AUTH_UPSTREAM: 'http://auth.internal',
      GOAL_SANDBOX: 'cloudflare',
      GOAL_SANDBOX_URL: 'http://sandbox.internal',
      // Only this Worker reaches goald's port 8080, and it writes X-Forwarded-For itself: goald
      // believes it, so the auth service's rate limits see each client (phase 4 review, M3).
      GOAL_TRUSTED_PROXIES: '0.0.0.0/0,::/0',
    });
    // The auth service's secrets and anything unlisted stay out of goald (AGENTS.md rule 7).
    for (const name of ['BETTER_AUTH_SECRETS', 'GOAL_AUTH_DATABASE_URL', 'UNLISTED_SETTING']) {
      expect(started).not.toHaveProperty(name);
    }
    expect(goald().intercepts).toEqual(['auth.internal', 'sandbox.internal']);
    expect(goald().started[0]?.enableInternet).toBe(true);
  });

  it('starts the auth service with its own settings only, for goald’s auth.internal', async () => {
    echo(auth());
    const res = await exports.GoaldOutbound.fetch('http://auth.internal/api/auth/get-session');
    expect(await res.json()).toMatchObject({ port: 8081, path: '/api/auth/get-session' });

    const started = auth().started[0]?.env ?? {};
    expect(started).toMatchObject({
      GOAL_AUTH_LISTEN: '0.0.0.0:8081',
      BETTER_AUTH_SECRETS: '1:better-auth-secret-for-tests',
      GOAL_AUTH_PROXY_SECRET: 'proxy-secret-for-tests',
    });
    for (const name of [
      'GOAL_MASTER_KEYS',
      'GOAL_DATABASE_URL',
      'GOAL_SANDBOX_KEY',
      'GOAL_GATEWAY_KEY',
      'UNLISTED_SETTING',
    ]) {
      expect(started).not.toHaveProperty(name);
    }
  });

  it('sends a box’s gateway.internal to goald’s gateway listener and nothing else', async () => {
    echo(goald());
    const res = await exports.BoxOutbound.fetch('http://gateway.internal/run');
    expect(await res.json()).toMatchObject({ port: 8090, path: '/run' });
    expect((await exports.BoxOutbound.fetch('http://sandbox.internal/v1/sandboxes')).status).toBe(404);
  });

  it('answers nothing but the auth service and the sandbox API to goald', async () => {
    expect((await exports.GoaldOutbound.fetch('http://gateway.internal/run')).status).toBe(404);
  });

  it('keeps goald running from the cron trigger', async () => {
    const c = goald();
    await c.destroy();
    echo(c);
    const worker = (await import('../src/index.js')).default;
    await worker.scheduled(createScheduledController(), env);
    expect(c.running).toBe(true);
  });
});

describe('docs', () => {
  it('still sends /v1/health to goald', async () => {
    echo(goald());
    const res = await exports.default.fetch('https://goal.example.com/v1/health');
    expect(await res.json()).toMatchObject({ port: 8080, path: '/v1/health' });
  });

  it('answers /docs/ from the static assets, not goald', async () => {
    const c = goald();
    c.port = async () => new Response('goald', { status: 500 });
    const res = await exports.default.fetch('https://goal.example.com/docs/');
    expect(res.status).toBe(200);
    expect(await res.text()).toContain('docs index stand-in');
  });

  it('answers a missing docs page with the site’s 404 page, never goald', async () => {
    const c = goald();
    c.port = async () => new Response('goald', { status: 500 });
    const res = await exports.default.fetch('https://goal.example.com/docs/missing');
    expect(res.status).toBe(404);
    expect(await res.text()).toContain('docs 404 stand-in');
  });

  it('leaves a path that only starts like /docs to goald', async () => {
    echo(goald());
    const res = await exports.default.fetch('https://goal.example.com/docsearch');
    expect(await res.json()).toMatchObject({ port: 8080, path: '/docsearch' });
  });
});
