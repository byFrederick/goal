// How the service worker answers a request. Pure: it reads nothing but its arguments, so vitest and
// both tsconfigs can import it. docs/engineering/frontend.md (PWA) holds the table.

export type Route = 'bypass' | 'network-first-shell' | 'cache-first' | 'stale-while-revalidate';

export interface RequestShape {
  url: URL;
  method: string;
  mode: string;
}

// Authenticated API answers and the identity service's routes (sign-in, sessions, the OAuth
// callbacks that are top-level navigations) are never cached and never answered with the shell.
const PRIVATE_PREFIXES = ['/v1', '/api', '/mcp'];
const BYPASS_PATHS = ['/sw.js', '/manifest.webmanifest'];
const STATIC_PATHS = ['/favicon.svg', '/theme.js', '/apple-touch-icon.png'];
const STATIC_PREFIXES = ['/icons/'];
const IMMUTABLE_PREFIX = '/assets/';

function under(path: string, prefix: string): boolean {
  return path === prefix || path.startsWith(`${prefix}/`);
}

/**
 * The route of one request made from the origin the worker serves. A non-GET or cross-origin
 * request, and anything under the private prefixes, goes to the network with no respondWith.
 */
export function routeRequest({ url, method, mode }: RequestShape, origin: string): Route {
  if (method !== 'GET' || url.origin !== origin) return 'bypass';

  const path = url.pathname;
  if (PRIVATE_PREFIXES.some((prefix) => under(path, prefix))) return 'bypass';
  if (BYPASS_PATHS.includes(path)) return 'bypass';

  if (mode === 'navigate') return 'network-first-shell';
  if (path.startsWith(IMMUTABLE_PREFIX)) return 'cache-first';
  if (STATIC_PATHS.includes(path) || STATIC_PREFIXES.some((prefix) => path.startsWith(prefix))) {
    return 'stale-while-revalidate';
  }
  return 'bypass';
}
