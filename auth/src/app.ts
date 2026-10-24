import { Hono } from 'hono';
import type { Auth } from './auth.js';

/**
 * createApp serves Better Auth under /api/auth, the paths goald's proxy forwards (its allowlist
 * decides which reach the service), and GET /api/auth/ok, the health check, on loopback only.
 * Everything else is 404.
 */
export function createApp(auth: Pick<Auth, 'handler'>): Hono {
  const app = new Hono();
  app.on(['GET', 'POST'], '/api/auth/*', (c) => auth.handler(c.req.raw));
  app.notFound((c) => c.json({ code: 'NOT_FOUND' }, 404));
  return app;
}
