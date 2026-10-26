// The service worker: precaches the app shell and caches the content-hashed build. It is built
// as /sw.js by vite.config.ts (its own classic script, with __BUILD_ID__ set to a hash of the build),
// and it answers only the requests routes.ts sends it. Typed against the WebWorker lib in
// src/sw/tsconfig.json, not the app's DOM lib.
import { routeRequest } from './routes';

declare const __BUILD_ID__: string;

const sw = self as unknown as ServiceWorkerGlobalScope;
const CACHE = `goal-shell-${__BUILD_ID__}`;
const SHELL = '/';

// Only the app's own answers are kept: a failed or cross-origin response never becomes the shell.
function cacheable(response: Response): boolean {
  return response.ok && response.type === 'basic';
}

async function precacheShell(): Promise<void> {
  const cache = await caches.open(CACHE);
  // The shell is the build being installed, so it comes from the network, not the HTTP cache.
  const response = await fetch(new Request(SHELL, { cache: 'reload' }));
  if (!cacheable(response)) throw new Error(`the app shell answered ${response.status}`);
  await cache.put(SHELL, response);
}

// A navigation is the app shell from the network, and the cached shell only when the network
// fails, so an offline launch still opens the app and the router takes it from there.
async function networkFirstShell(request: Request): Promise<Response> {
  try {
    return await fetch(request);
  } catch (err) {
    const shell = await (await caches.open(CACHE)).match(SHELL);
    if (shell) return shell;
    throw err;
  }
}

async function cacheFirst(request: Request): Promise<Response> {
  const cache = await caches.open(CACHE);
  const hit = await cache.match(request);
  if (hit) return hit;
  const response = await fetch(request);
  if (cacheable(response)) await cache.put(request, response.clone());
  return response;
}

// The cached copy answers at once and the network refreshes it behind the answer, so the next
// load has the newest copy.
async function staleWhileRevalidate(event: FetchEvent, request: Request): Promise<Response> {
  const cache = await caches.open(CACHE);
  const hit = await cache.match(request);
  const refresh = fetch(request).then(async (response) => {
    if (cacheable(response)) await cache.put(request, response.clone());
    return response;
  });
  if (hit) {
    event.waitUntil(refresh.catch(() => undefined));
    return hit;
  }
  return refresh;
}

sw.addEventListener('install', (event) => {
  // No skipWaiting here: a new build waits until the page asks for it (the update prompt).
  event.waitUntil(precacheShell());
});

sw.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      for (const key of await caches.keys()) {
        if (key.startsWith('goal-shell-') && key !== CACHE) await caches.delete(key);
      }
      await sw.clients.claim();
    })(),
  );
});

sw.addEventListener('message', (event: ExtendableMessageEvent) => {
  if (event.data?.type === 'SKIP_WAITING') void sw.skipWaiting();
});

sw.addEventListener('fetch', (event) => {
  const { request } = event;
  const route = routeRequest(
    { url: new URL(request.url), method: request.method, mode: request.mode },
    sw.location.origin,
  );
  // A bypass returns before respondWith, so the browser handles it as if there were no worker.
  switch (route) {
    case 'bypass':
      return;
    case 'network-first-shell':
      event.respondWith(networkFirstShell(request));
      return;
    case 'cache-first':
      event.respondWith(cacheFirst(request));
      return;
    case 'stale-while-revalidate':
      event.respondWith(staleWhileRevalidate(event, request));
      return;
  }
});
