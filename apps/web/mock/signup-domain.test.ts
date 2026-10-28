import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { OrgSettings, Problem, SignupDomain } from '@goal/api-client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';

// The mock answers PUT and DELETE /settings/signup-domain the way goald does (ADR 0023, decision 7):
// an owner with a recent sign-in, the caller's own domain, no public webmail, one organization
// only. The mock's default owner is f.cruz@example.com, who owns acme and goal.

let api: MockApi;
let server: Server;
let base = '';
let ownerCookie = '';

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

/** Resets the mock and signs someone in; the session cookie is the caller's from then on. */
async function signInAs(email: string, signedIn: 'fresh' | 'stale' = 'fresh'): Promise<string> {
  const res = await fetch(`${base}/__mock/reset`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ auth: { mode: 'session', signed_in: signedIn, email } }),
  });
  const cookie = res.headers.get('set-cookie') ?? '';
  return cookie.split(';')[0] ?? '';
}

beforeEach(async () => {
  ownerCookie = await signInAs('f.cruz@example.com');
});

async function call<T>(path: string, init: RequestInit = {}, cookie = ownerCookie) {
  const res = await fetch(`${base}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', Cookie: cookie, ...(init.headers ?? {}) },
  });
  const text = await res.text();
  return { status: res.status, body: (text ? JSON.parse(text) : undefined) as T };
}

const signupPath = '/v1/orgs/acme/settings/signup-domain';
const put = (body: unknown, cookie?: string) =>
  call<SignupDomain | Problem>(signupPath, { method: 'PUT', body: JSON.stringify(body) }, cookie);

describe('PUT /settings/signup-domain', () => {
  it('sets the caller’s own domain to join as a viewer, and GET settings shows it', async () => {
    const set = await put({ domain: 'Example.com' });
    expect(set.status).toBe(200);
    expect(set.body).toMatchObject({ domain: 'example.com', join_role: 'viewer', set_by: { name: 'F. Cruz' } });

    const settings = await call<OrgSettings>('/v1/orgs/acme/settings');
    expect(settings.body.signup_domain).toMatchObject({ domain: 'example.com', join_role: 'viewer' });
  });

  it('audits the change as org.signup_domain_set', async () => {
    await put({ domain: 'example.com' });
    const audit = await call<{ entries: { action: string }[] }>('/v1/orgs/acme/audit');
    expect(audit.body.entries.map((e) => e.action)).toContain('org.signup_domain_set');
  });

  it('changes nothing when the domain is already set', async () => {
    await put({ domain: 'example.com' });
    const again = await put({ domain: 'example.com' });
    expect(again.status).toBe(200);
    const audit = await call<{ entries: { action: string }[] }>('/v1/orgs/acme/audit?action=org.signup_domain_set');
    expect(audit.body.entries).toHaveLength(1);
  });

  it('refuses a domain the caller’s email is not at', async () => {
    const res = await put({ domain: 'northwind.dev' });
    expect(res.status).toBe(400);
    expect((res.body as Problem).code).toBe('signup-domain-not-yours');
  });

  it('refuses a malformed domain or an unknown role without a code the page reads', async () => {
    const res = await put({ domain: 'not a domain' });
    expect(res.status).toBe(400);
    expect((res.body as Problem).code).toBeUndefined();
  });

  it('refuses a domain another organization holds, with 409', async () => {
    await put({ domain: 'example.com' });
    const other = await call<Problem>('/v1/orgs/goal/settings/signup-domain', {
      method: 'PUT',
      body: JSON.stringify({ domain: 'example.com' }),
    });
    expect(other.status).toBe(409);
    expect(other.body.code).toBe('signup-domain-taken');
  });

  it('refuses a caller who is not an owner, with 403', async () => {
    const admin = await signInAs('m.okafor@example.com');
    const res = await put({ domain: 'example.com' }, admin);
    expect(res.status).toBe(403);
    expect((res.body as Problem).code).toBe('forbidden');
  });

  it('asks an owner whose sign-in is old to sign in again, with 403 reauth-required', async () => {
    const stale = await signInAs('f.cruz@example.com', 'stale');
    const res = await put({ domain: 'example.com' }, stale);
    expect(res.status).toBe(403);
    expect((res.body as Problem).code).toBe('reauth-required');
  });
});

describe('DELETE /settings/signup-domain', () => {
  it('clears the domain with 204 and audits org.signup_domain_cleared', async () => {
    await put({ domain: 'example.com' });
    const res = await call('/v1/orgs/acme/settings/signup-domain', { method: 'DELETE' });
    expect(res.status).toBe(204);
    const settings = await call<OrgSettings>('/v1/orgs/acme/settings');
    expect(settings.body.signup_domain).toBeUndefined();
    const audit = await call<{ entries: { action: string }[] }>('/v1/orgs/acme/audit');
    expect(audit.body.entries.map((e) => e.action)).toContain('org.signup_domain_cleared');
  });

  it('is no change when nothing is set', async () => {
    const res = await call('/v1/orgs/acme/settings/signup-domain', { method: 'DELETE' });
    expect(res.status).toBe(204);
  });
});
