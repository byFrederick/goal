import { createHash, timingSafeEqual } from 'node:crypto';
import { Hono } from 'hono';
import type { Auth } from './auth.js';

/** The header goald's proxy sends the shared secret in (GOAL_AUTH_PROXY_SECRET). */
export const proxySecretHeader = 'x-goal-proxy-secret';

const healthPath = '/api/auth/ok';

/**
 * createApp serves Better Auth under /api/auth, the paths goald's proxy forwards (its allowlist
 * decides which reach the service), and GET /api/auth/ok, the health check, on loopback only.
 * Every request but the health check must carry proxySecret in X-Goal-Proxy-Secret, as goald's
 * proxy sends it; anything else is 403, before Better Auth sees it. Everything else is 404.
 */
export function createApp(auth: Pick<Auth, 'handler'>, proxySecret: string): Hono {
  const app = new Hono();
  const want = digest(proxySecret);
  app.use('/api/auth/*', async (c, next) => {
    if (c.req.method === 'GET' && c.req.path === healthPath) return next();
    const got = c.req.header(proxySecretHeader);
    if (got === undefined || !timingSafeEqual(digest(got), want)) return c.json({ code: 'FORBIDDEN' }, 403);
    return next();
  });
  app.on(['GET', 'POST'], '/api/auth/*', (c) => auth.handler(c.req.raw));
  app.notFound((c) => c.json({ code: 'NOT_FOUND' }, 404));
  return app;
}

/** digest compares secrets of any length in constant time. */
function digest(s: string): Buffer {
  return createHash('sha256').update(s).digest();
}
