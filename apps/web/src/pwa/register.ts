import { toast } from 'sonner';

// Production builds only: the worker (src/sw/sw.ts) caches the build it came with, and under vite
// dev the modules change under it on every save. docs/engineering/frontend.md (PWA) has the flow.
const UPDATE_TOAST = 'goal-update';
// A worker that has no GET_BUILD handler (one from before this check) never answers.
const BUILD_ANSWER_MS = 2000;
// How long a worker that did not answer waits for the update check's install to settle.
const UPDATE_SETTLE_MS = 4000;

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

// Resolves once the worker being installed has finished (or failed) installing, or after the wait.
function installSettled(registration: ServiceWorkerRegistration): Promise<void> {
  const installing = registration.installing;
  if (installing?.state !== 'installing') return Promise.resolve();
  return new Promise((resolve) => {
    const done = () => {
      clearTimeout(timer);
      resolve();
    };
    const timer = setTimeout(done, UPDATE_SETTLE_MS);
    installing.addEventListener('statechange', () => {
      if (installing.state !== 'installing') done();
    });
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

  container
    .register('/sw.js', { scope: '/' })
    .then((registration) => {
      // A browser that blocks workers (a test browser, a privacy setting) can resolve with nothing.
      if (!registration) return;

      // The worker the open toast was offered for.
      let offered: ServiceWorker | null = null;
      const closeToast = () => {
        offered = null;
        toast.dismiss(UPDATE_TOAST);
      };
      const offer = (worker: ServiceWorker) => {
        offered = worker;
        worker.addEventListener('statechange', () => {
          if (worker.state !== 'redundant' || offered !== worker) return;
          // Replaced while the toast was open: its Reload would do nothing. Decide again.
          closeToast();
          if (registration.waiting) handleWaiting(registration.waiting);
        });
        toast('A new version of Goal is ready', {
          id: UPDATE_TOAST,
          duration: Number.POSITIVE_INFINITY,
          action: {
            label: 'Reload',
            onClick: () => {
              if (worker.state === 'installed') {
                worker.postMessage({ type: 'SKIP_WAITING' });
                return;
              }
              // No longer waiting: a newer worker took over, so reloading loads what it serves.
              if (reloading) return;
              reloading = true;
              window.location.reload();
            },
          },
        });
      };

      // A waiting worker of the build the page already runs is not news: it takes over quietly. A
      // different build is offered with the toast. One that does not answer is older than this page,
      // so the update check runs first: the worker that replaces it is the one to decide for.
      const seen = new WeakSet<ServiceWorker>();
      const handleWaiting = (worker: ServiceWorker) => {
        if (seen.has(worker)) return;
        seen.add(worker);
        void workerBuild(worker).then(async (build) => {
          const page = pageBuild();
          if (build !== null && page !== null && build === page) {
            silentActivation = true;
            closeToast();
            worker.postMessage({ type: 'SKIP_WAITING' });
            return;
          }
          if (build !== null) {
            offer(worker);
            return;
          }
          await registration.update().catch(() => undefined);
          await installSettled(registration);
          const current = registration.waiting;
          if (current === worker) offer(worker);
          else if (current) handleWaiting(current);
        });
      };

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
