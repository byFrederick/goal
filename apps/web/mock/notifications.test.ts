import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Connection, NotificationPreferences, Problem } from '@goal/api-client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

// The mock answers GET and PUT /v1/me/notifications the way goald does (ADR 0023, decision 1): a person's
// own preferences, seeded with the picture's boxes; a refused email or time zone is 400; GOAL_AUTH=none's
// local user is 403 person-required. The Slack connection the account page needs is seeded in acme.

let api: MockApi;
let server: Server;
let base = '';
let cookie = '';

beforeAll(async () => {
  api = createMockApi();
  server = createServer((req, res) =>
    api.handle(req, res, () => {
      res.statusCode = 404;
      res.end();
    }),
  );
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

afterAll(async () => {
  api.close();
  await new Promise((resolve) => server.close(resolve));
});

/** Resets the mock to a session for the person, and keeps their cookie. */
async function signInAs(email: string): Promise<string> {
  const res = await fetch(`${base}/__mock/reset`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ auth: { mode: 'session', signed_in: 'fresh', email } }),
  });
  return (res.headers.get('set-cookie') ?? '').split(';')[0] ?? '';
}

async function call<T>(path: string, init: RequestInit = {}) {
  const res = await fetch(`${base}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', Cookie: cookie, ...(init.headers ?? {}) },
  });
  const text = await res.text();
  return { status: res.status, body: (text ? JSON.parse(text) : undefined) as T };
}

const prefsPath = '/v1/me/notifications';
const put = (body: unknown) =>
  call<NotificationPreferences | Problem>(prefsPath, { method: 'PUT', body: JSON.stringify(body) });

beforeEach(async () => {
  cookie = await signInAs('f.cruz@example.com');
});

describe('GET and PUT /v1/me/notifications', () => {
  it('serves the seeded boxes of the picture, with a UTC time zone and no email', async () => {
    const got = await call<NotificationPreferences>(prefsPath);
    expect(got.status).toBe(200);
    expect(got.body.events.approval).toEqual({ in_goal: true, email: false, slack: true });
    expect(got.body.events.digest).toEqual({ in_goal: false, email: true, slack: false });
    expect(got.body).toMatchObject({ email: '', time_zone: 'UTC' });
  });

  it('keeps what was saved for the person, and serves it on the next read', async () => {
    const current = (await call<NotificationPreferences>(prefsPath)).body;
    const next = {
      ...current,
      email: 'freddy@example.com',
      events: { ...current.events, budget: { in_goal: true, email: true, slack: true } },
    };
    expect((await put(next)).status).toBe(200);
    expect((await call<NotificationPreferences>(prefsPath)).body).toEqual(next);
  });

  it('refuses an email that is not an address, and saves nothing', async () => {
    const current = (await call<NotificationPreferences>(prefsPath)).body;
    const res = await put({ ...current, email: 'not an address' });
    expect(res.status).toBe(400);
    expect(res.body).toMatchObject({ code: 'invalid-notification-email' });
  });

  it('refuses a digest with In Goal', async () => {
    const current = (await call<NotificationPreferences>(prefsPath)).body;
    const res = await put({
      ...current,
      events: { ...current.events, digest: { in_goal: true, email: true, slack: false } },
    });
    expect(res.body).toMatchObject({ code: 'invalid-notification-preferences' });
  });

  it('refuses a time zone that is not an IANA name', async () => {
    const current = (await call<NotificationPreferences>(prefsPath)).body;
    const res = await put({ ...current, time_zone: 'Mars/Olympus' });
    expect(res.body).toMatchObject({ code: 'invalid-time-zone' });
  });

  it('refuses a local user under GOAL_AUTH=none with person-required', async () => {
    await call('/__mock/reset', { method: 'POST', body: JSON.stringify({ auth: { mode: 'none' } }) });
    const res = await call<Problem>(prefsPath);
    expect(res.status).toBe(403);
    expect(res.body).toMatchObject({ code: 'person-required' });
  });

  it('seeds a Slack connection in acme, and lists it by kind', async () => {
    const slack = await call<{ connections: Connection[] }>('/v1/orgs/acme/connections?kind=slack');
    expect(slack.body.connections.map((c) => c.kind)).toEqual(['slack']);
  });
});
