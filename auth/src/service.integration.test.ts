import { randomBytes } from 'node:crypto';
import { createServer } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { RoleError } from './db.js';
import type { Logger } from './log.js';
import { MemoryMailer } from './mail/memory.js';
import { startService } from './service.js';
import { createTestDatabase, type TestDatabase } from './testing/database.js';

const silent: Logger = { log: () => {} };
const noMail = new MemoryMailer();

let head: TestDatabase;
let beforeBetterAuth: TestDatabase;

beforeAll(async () => {
  [head, beforeBetterAuth] = await Promise.all([createTestDatabase(), createTestDatabase('00040')]);
});

afterAll(async () => {
  await Promise.all([head?.drop(), beforeBetterAuth?.drop()]);
});

async function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const s = createServer();
    s.once('error', reject);
    s.listen(0, '127.0.0.1', () => {
      const address = s.address();
      s.close(() => (typeof address === 'object' && address ? resolve(address.port) : reject(new Error('no port'))));
    });
  });
}

async function env(databaseURL: string) {
  return {
    GOAL_AUTH_DATABASE_URL: databaseURL,
    GOAL_AUTH_LISTEN: `127.0.0.1:${await freePort()}`,
    BETTER_AUTH_SECRET: randomBytes(32).toString('hex'),
  };
}

describe('startService', () => {
  it('starts as goal_auth on a database at the goose head and answers its health check', async () => {
    const service = await startService(await env(head.url('goal_auth')), silent, noMail);
    try {
      const res = await fetch(`${service.url}/api/auth/ok`);

      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ ok: true });
      expect((await fetch(`${service.url}/v1/health`)).status).toBe(404);
    } finally {
      await service.close();
    }
  });

  it.each([
    ['the superuser', undefined, 'is a superuser'],
    ['goal_owner', 'goal_owner', 'owns tables'],
    ['goal_app', 'goal_app', 'is a member of goal_app'],
  ])('refuses to run as %s', async (_, role, reason) => {
    const started = startService(await env(head.url(role)), silent, noMail);

    await expect(started).rejects.toBeInstanceOf(RoleError);
    await expect(started).rejects.toThrow(reason);
  });

  it('refuses a database without the schema this Better Auth expects', async () => {
    await expect(startService(await env(beforeBetterAuth.url('goal_auth')), silent, noMail)).rejects.toThrow(/schema/i);
  });
});
