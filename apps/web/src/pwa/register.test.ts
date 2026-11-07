import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const toastMock = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: toastMock }));

import { registerServiceWorker } from './register';

type Listener = () => void;

// A worker that answers GET_BUILD on the transferred port with its build, or stays silent.
function fakeWorker(build: string | null) {
  const worker = {
    state: 'installed',
    posted: [] as string[],
    listeners: new Map<string, Listener>(),
    addEventListener(type: string, fn: Listener) {
      worker.listeners.set(type, fn);
    },
    postMessage(message: { type: string }, transfer?: MessagePort[]) {
      worker.posted.push(message.type);
      if (message.type === 'GET_BUILD' && build !== null) transfer?.[0]?.postMessage({ type: 'BUILD', build });
    },
  };
  return worker;
}

function setup(options: { page: string | null; waiting?: ReturnType<typeof fakeWorker>; controlled?: boolean }) {
  const containerListeners = new Map<string, Listener>();
  const registrationListeners = new Map<string, Listener>();
  const docListeners = new Map<string, Listener>();
  const registration = {
    waiting: options.waiting ?? null,
    installing: null as ReturnType<typeof fakeWorker> | null,
    update: vi.fn(() => Promise.resolve()),
    addEventListener: (type: string, fn: Listener) => registrationListeners.set(type, fn),
  };
  const reload = vi.fn();
  vi.stubGlobal('navigator', {
    serviceWorker: {
      controller: options.controlled === false ? null : {},
      register: vi.fn(() => Promise.resolve(registration)),
      addEventListener: (type: string, fn: Listener) => containerListeners.set(type, fn),
    },
  });
  vi.stubGlobal('window', { location: { reload } });
  vi.stubGlobal('document', {
    visibilityState: 'visible',
    querySelector: () => (options.page === null ? null : { getAttribute: () => options.page }),
    addEventListener: (type: string, fn: Listener) => docListeners.set(type, fn),
  });
  return { containerListeners, registrationListeners, docListeners, registration, reload };
}

const flush = async () => {
  for (let i = 0; i < 5; i++) await vi.advanceTimersByTimeAsync(10);
};

describe('registerServiceWorker', () => {
  beforeEach(() => {
    vi.stubEnv('PROD', true);
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    toastMock.mockClear();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it('activates a waiting worker of the page build with no toast and no reload', async () => {
    const waiting = fakeWorker('aaaa');
    const env = setup({ page: 'aaaa', waiting });
    registerServiceWorker();
    await flush();
    expect(waiting.posted).toEqual(['GET_BUILD', 'SKIP_WAITING']);
    env.containerListeners.get('controllerchange')?.();
    expect(env.reload).not.toHaveBeenCalled();
    expect(toastMock).not.toHaveBeenCalled();
  });

  it('activates quietly a worker of the page build that finishes installing later', async () => {
    const env = setup({ page: 'aaaa' });
    registerServiceWorker();
    await flush();
    const installing = fakeWorker('aaaa');
    env.registration.installing = installing;
    env.registrationListeners.get('updatefound')?.();
    env.registration.waiting = installing;
    installing.listeners.get('statechange')?.();
    await flush();
    expect(installing.posted).toEqual(['GET_BUILD', 'SKIP_WAITING']);
    env.containerListeners.get('controllerchange')?.();
    expect(env.reload).not.toHaveBeenCalled();
    expect(toastMock).not.toHaveBeenCalled();
  });

  it('offers the toast for a newer build, and Reload activates it and reloads once', async () => {
    const waiting = fakeWorker('bbbb');
    const env = setup({ page: 'aaaa', waiting });
    registerServiceWorker();
    await flush();
    expect(toastMock).toHaveBeenCalledTimes(1);
    expect(waiting.posted).toEqual(['GET_BUILD']);
    const options = toastMock.mock.calls[0]?.[1] as { action: { onClick: () => void } };
    options.action.onClick();
    expect(waiting.posted).toEqual(['GET_BUILD', 'SKIP_WAITING']);
    env.containerListeners.get('controllerchange')?.();
    env.containerListeners.get('controllerchange')?.();
    expect(env.reload).toHaveBeenCalledTimes(1);
  });

  it('offers the toast when the worker does not answer', async () => {
    const waiting = fakeWorker(null);
    setup({ page: 'aaaa', waiting });
    registerServiceWorker();
    await flush();
    expect(toastMock).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(2000);
    expect(toastMock).toHaveBeenCalledTimes(1);
  });

  it('checks for an update on load and when the page comes back to the front', async () => {
    const env = setup({ page: 'aaaa' });
    registerServiceWorker();
    await flush();
    expect(env.registration.update).toHaveBeenCalledTimes(1);
    env.docListeners.get('visibilitychange')?.();
    expect(env.registration.update).toHaveBeenCalledTimes(2);
  });

  it('shows nothing and reloads nothing on a first visit', async () => {
    const env = setup({ page: 'aaaa', controlled: false });
    registerServiceWorker();
    await flush();
    env.containerListeners.get('controllerchange')?.();
    expect(env.reload).not.toHaveBeenCalled();
    expect(toastMock).not.toHaveBeenCalled();
  });
});
