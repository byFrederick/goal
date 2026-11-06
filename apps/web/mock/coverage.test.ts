import { readdirSync, readFileSync, statSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { join } from 'node:path';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { createMockApi, type MockApi } from './api.ts';
import { unserved } from './unserved.ts';

// Keeps the mock from falling behind the web app: every call the app makes through the API client
// (`goal.GET('/v1/…')`, `goal.POST(orgPath('/…'))`) is made on the mock with seeded parameters, and the mock's
// unmatched-route fallthrough marks its answer with the code `mock-unserved`. A call it does not serve must be
// listed in `unserved.ts` with the reason, and an entry the mock serves or the app no longer calls fails the test.

const methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'];

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const file = join(dir, name);
    if (statSync(file).isDirectory()) return sources(file);
    return /\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) ? [file] : [];
  });
}

/** Every call of the web app, as 'METHOD /v1/…' with the contract's path. */
function calls(): string[] {
  const found = new Set<string>();
  const call = /goal\.(GET|POST|PUT|PATCH|DELETE)\(\s*(orgPath\('([^']*)'\)|'([^']*)')/g;
  for (const file of sources(join(import.meta.dirname, '..', 'src'))) {
    for (const m of readFileSync(file, 'utf8').matchAll(call)) {
      const path = m[3] !== undefined ? `/v1/orgs/{org}${m[3]}` : m[4];
      found.add(`${m[1]} ${path}`);
    }
  }
  return [...found].sort();
}

let api: MockApi;
let server: Server;
let base = '';

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

describe('the mock API against the calls of the web app', () => {
  it('serves every call, or lists it in `unserved` with a reason', async () => {
    await fetch(`${base}/__mock/reset`, { method: 'POST', body: '{}' });
    const factories = (await (await fetch(`${base}/v1/orgs/acme/factories`)).json()) as {
      factories: { id: string; name: string }[];
    };
    const software = factories.factories.find((f) => f.name === 'software')?.id ?? 'software';

    const all = calls();
    expect(all.length).toBeGreaterThanOrEqual(100);

    const missing: string[] = [];
    const served = new Set<string>();
    for (const key of all) {
      const [method = 'GET', path = ''] = key.split(' ');
      const url = path
        .replace('{org}', 'acme')
        .replace('{factory}', software)
        .replace('{number}', '1')
        .replace(/\{[^}]+\}/g, 'x1');
      const res = await fetch(`${base}${url}`, {
        method,
        headers: { 'Content-Type': 'application/json' },
        ...(method === 'GET' ? {} : { body: '{}' }),
      });
      const answer = (await res.json().catch(() => null)) as { code?: string } | null;
      if (res.status === 404 && (answer === null || answer.code === 'mock-unserved')) {
        if (!unserved[key]) missing.push(key);
      } else served.add(key);
    }
    expect(missing, 'calls the mock answers mock-unserved; serve them or list them in `unserved`').toEqual([]);

    const stale = Object.keys(unserved).filter((key) => served.has(key) || !all.includes(key));
    expect(stale, 'entries of `unserved` the mock now serves or the web no longer calls').toEqual([]);
    for (const [key, reason] of Object.entries(unserved)) {
      expect(methods.includes(key.split(' ')[0] ?? ''), key).toBe(true);
      expect(reason.trim(), key).not.toBe('');
    }
  });
});
