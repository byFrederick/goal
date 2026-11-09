// @vitest-environment jsdom
import { ApiError } from '@goal/api-client';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ErrorScreen, NotFoundScreen } from './states';

describe('the not-found screen', () => {
  it('names what is missing and offers one way back, without a card', async () => {
    const root = createRootRoute({ component: () => <NotFoundScreen what="item" /> });
    const router = createRouter({ routeTree: root, history: createMemoryHistory({ initialEntries: ['/'] }) });
    render(<RouterProvider router={router} />);

    expect(await screen.findByRole('heading', { level: 1, name: 'No such item' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Go to the floor' }).getAttribute('href')).toBe('/');
    const screenBox = screen.getByRole('heading').parentElement;
    expect(screenBox?.className).not.toMatch(/border|bg-card|\bp-5\b/);
  });
});

describe('the restarting screen', () => {
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('says Goal is restarting, loads again after Retry-After without a reload, and shows the page', async () => {
    const reload = vi.fn();
    vi.stubGlobal('location', { ...window.location, reload });
    let up = false;
    const root = createRootRoute();
    const page = createRoute({
      getParentRoute: () => root,
      path: '/',
      loader: () => {
        if (!up) throw new ApiError({ title: 'Goal is starting', status: 503, code: 'goald-starting' }, 3);
      },
      component: () => <h1>The floor</h1>,
      errorComponent: ErrorScreen,
    });
    const router = createRouter({
      routeTree: root.addChildren([page]),
      history: createMemoryHistory({ initialEntries: ['/'] }),
    });
    vi.useFakeTimers({ shouldAdvanceTime: true });
    render(<RouterProvider router={router} />);

    expect(await screen.findByRole('status')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Goal is restarting' })).toBeTruthy();
    expect(screen.getByText('This page loads again by itself.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();

    up = true;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_900);
    });
    expect(screen.queryByText('The floor')).toBeNull();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(await screen.findByRole('heading', { name: 'The floor' })).toBeTruthy();
    expect(screen.queryByRole('status')).toBeNull();
    expect(reload).not.toHaveBeenCalled();
    expect(router.state.location.pathname).toBe('/');
    vi.unstubAllGlobals();
  });
  it('waits 2, 4, 8 then 15 s between loads when a 503 has no Retry-After and keeps failing', async () => {
    const loads: number[] = [];
    const root = createRootRoute();
    const t0 = Date.now();
    const page = createRoute({
      getParentRoute: () => root,
      path: '/slow',
      loader: () => {
        loads.push(Math.round((Date.now() - t0) / 1000));
        throw new ApiError({ title: 'Service unavailable', status: 503 });
      },
      component: () => <h1>The floor</h1>,
      errorComponent: ErrorScreen,
    });
    const router = createRouter({
      routeTree: root.addChildren([page]),
      history: createMemoryHistory({ initialEntries: ['/slow'] }),
    });
    vi.useFakeTimers({ shouldAdvanceTime: false });
    render(<RouterProvider router={router} />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(loads).toEqual([0, 2, 6, 14, 29]);
  });
});
