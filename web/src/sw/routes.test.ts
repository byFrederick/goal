import { describe, expect, it } from 'vitest';
import { routeRequest } from './routes';

const origin = 'https://goal.example.com';

// A request as the worker sees it: the URL, the method and the fetch mode (navigate for a page load).
function route(path: string, { method = 'GET', mode = 'cors', from = origin } = {}) {
  return routeRequest({ url: new URL(path, origin), method, mode }, from);
}

describe('routeRequest', () => {
  it.each([
    ['GET /v1/orgs', route('/v1/orgs')],
    ['GET /v1 itself', route('/v1')],
    ['GET /api/auth/get-session', route('/api/auth/get-session')],
    ['GET /api/auth/sign-in/social as a fetch', route('/api/auth/sign-in/social')],
    ['GET /api as a fetch', route('/api', { mode: 'cors' })],
    ['a navigation to /api/auth/callback/google', route('/api/auth/callback/google', { mode: 'navigate' })],
    ['a navigation to /api/auth/sign-in', route('/api/auth/sign-in', { mode: 'navigate' })],
    ['a navigation to /v1/orgs', route('/v1/orgs', { mode: 'navigate' })],
    ['GET /mcp', route('/mcp')],
    ['GET /mcp/tools', route('/mcp/tools')],
    ['GET /sw.js', route('/sw.js', { mode: 'navigate' })],
    ['GET /manifest.webmanifest', route('/manifest.webmanifest')],
    ['a POST to /v1/orgs', route('/v1/orgs', { method: 'POST' })],
    ['a POST to /', route('/', { method: 'POST', mode: 'navigate' })],
    ['a PUT to /assets/app.js', route('/assets/app.js', { method: 'PUT' })],
    ['a navigation with method POST', route('/device', { method: 'POST', mode: 'navigate' })],
    ['a cross-origin GET', route('https://other.example.com/assets/app.js', { from: origin })],
    ['a cross-origin navigation', route('https://accounts.example.org/', { mode: 'navigate' })],
  ])('bypasses %s', (_, result) => {
    expect(result).toBe('bypass');
  });

  it.each([
    ['/', route('/', { mode: 'navigate' })],
    ['/o/acme/f/sre/board', route('/o/acme/f/sre/board', { mode: 'navigate' })],
    ['/device', route('/device', { mode: 'navigate' })],
    ['/sign-in', route('/sign-in', { mode: 'navigate' })],
    ['/invite/x', route('/invite/x', { mode: 'navigate' })],
  ])('answers a navigation to %s with the shell when the network fails', (_, result) => {
    expect(result).toBe('network-first-shell');
  });

  it.each(['/assets/index-abc123.js', '/assets/inter-latin-400.woff2', '/assets/index-9f8e.css'])(
    'caches %s first',
    (path) => {
      expect(route(path, { mode: 'no-cors' })).toBe('cache-first');
    },
  );

  it.each([
    '/favicon.svg',
    '/theme.js',
    '/icons/icon-192.png',
    '/apple-touch-icon.png',
    '/icons/icon-maskable-512.png',
  ])('revalidates %s while serving it from the cache', (path) => {
    expect(route(path, { mode: 'no-cors' })).toBe('stale-while-revalidate');
  });

  it('bypasses other same-origin GETs that are not static files', () => {
    expect(route('/robots.txt')).toBe('bypass');
  });

  it('does not match a path that only starts with a private prefix', () => {
    expect(route('/mcpx')).toBe('bypass');
    expect(route('/apiary', { mode: 'navigate' })).toBe('network-first-shell');
    expect(route('/v1x', { mode: 'navigate' })).toBe('network-first-shell');
  });
});
