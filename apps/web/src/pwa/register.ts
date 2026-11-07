import { toast } from 'sonner';

// Production builds only: the worker (src/sw/sw.ts) caches the build it came with, and under vite
// dev the modules change under it on every save. docs/engineering/frontend.md (PWA) has the flow.
const UPDATE_TOAST = 'goal-update';
// A worker that has no GET_BUILD handler (one from before this check) never answers.
const BUILD_ANSWER_MS = 2000;

// The build this page runs: vite writes it into index.html (vite.config.ts), which every load
// fetches from the network, so it is the id of the bundle in this page.
function pageBuild(): string | null {
  return document.querySelector('meta[name="goal-build"]')?.getAttribute('content') ?? null;
}

// The waiting worker's own build id, or null when it does not answer in time.
function workerBuild(worker: ServiceWorker): Promise<string | null> {
  return new Promise((resolve) => {
    const channel = new MessageChannel();
    const done = (build: string | null) => {
      clearTimeout(timer);
      channel.port1.close();
      resolve(build);
    };
    const timer = setTimeout(() => done(null), BUILD_ANSWER_MS);
    channel.port1.onmessage = (event: MessageEvent) => {
      const build = (event.data as { build?: unknown } | null)?.build;
      done(typeof build === 'string' ? build : null);
    };
    worker.postMessage({ type: 'GET_BUILD' }, [channel.port2]);
  });
}

export function registerServiceWorker(): void {
  if (!import.meta.env.PROD || !('serviceWorker' in navigator)) return;

  const container = navigator.serviceWorker;
  // A first visit has no controller, and the worker that claims the page then is not an update:
  // the page already runs the build it loaded. Any later change of controller is a new build.
  let controlled = container.controller !== null;
  let reloading = false;
  // Set before a same-build worker is told to activate: its controllerchange is not a new build.
  let silentActivation = false;
  container.addEventListener('controllerchange', () => {
    if (!controlled) {
      controlled = true;
      return;
    }
    if (silentActivation) {
      silentActivation = false;
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

  // A waiting worker of the build the page already runs is not news: it takes over quietly. Any
  // other build, or one that does not say which it is, is offered with the toast.
  const seen = new WeakSet<ServiceWorker>();
  const handleWaiting = (worker: ServiceWorker) => {
    if (seen.has(worker)) return;
    seen.add(worker);
    void workerBuild(worker).then((build) => {
      const page = pageBuild();
      if (build !== null && page !== null && build === page) {
        silentActivation = true;
        worker.postMessage({ type: 'SKIP_WAITING' });
        return;
      }
      offer(worker);
    });
  };

  container
    .register('/sw.js', { scope: '/' })
    .then((registration) => {
      // A browser that blocks workers (a test browser, a privacy setting) can resolve with nothing.
      if (!registration) return;
      if (registration.waiting && controlled) handleWaiting(registration.waiting);
      registration.addEventListener('updatefound', () => {
        const installing = registration.installing;
        installing?.addEventListener('statechange', () => {
          if (installing.state === 'installed' && registration.waiting && controlled) {
            handleWaiting(registration.waiting);
          }
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
