import { toast } from 'sonner';

// Production builds only: the worker (src/sw/sw.ts) caches the build it came with, and under vite
// dev the modules change under it on every save. docs/engineering/frontend.md (PWA) has the flow.
const UPDATE_TOAST = 'goal-update';

export function registerServiceWorker(): void {
  if (!import.meta.env.PROD || !('serviceWorker' in navigator)) return;

  const container = navigator.serviceWorker;
  // A first visit has no controller, and the worker that claims the page then is not an update:
  // the page already runs the build it loaded. Any later change of controller is a new build.
  let controlled = container.controller !== null;
  let reloading = false;
  container.addEventListener('controllerchange', () => {
    if (!controlled) {
      controlled = true;
      return;
    }
    if (reloading) return;
    reloading = true;
    window.location.reload();
  });

  const offer = (worker: ServiceWorker) => {
    toast('A new version of Goal is ready', {
      id: UPDATE_TOAST,
      duration: Number.POSITIVE_INFINITY,
      action: { label: 'Reload', onClick: () => worker.postMessage({ type: 'SKIP_WAITING' }) },
    });
  };

  container
    .register('/sw.js', { scope: '/' })
    .then((registration) => {
      if (registration.waiting && controlled) offer(registration.waiting);
      registration.addEventListener('updatefound', () => {
        const installing = registration.installing;
        installing?.addEventListener('statechange', () => {
          if (installing.state === 'installed' && registration.waiting && controlled) offer(registration.waiting);
        });
      });
      // A phone keeps the app open for days: look again whenever the page comes back to the front.
      const check = () => void registration.update();
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') check();
      });
      check();
    })
    .catch((err: unknown) => {
      console.error('Goal could not register its service worker', err);
    });
}
