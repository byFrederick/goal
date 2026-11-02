// @vitest-environment jsdom
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { ShellPending } from './shell-pending';

async function renderAt(path: string) {
  const root = createRootRoute();
  const page = createRoute({
    getParentRoute: () => root,
    path: '/o/$org/f/$factory/agents',
    component: ShellPending,
  });
  const router = createRouter({
    routeTree: root.addChildren([page]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  const view = render(<RouterProvider router={router} />);
  await screen.findAllByText('Loading');
  return view;
}

describe('the shell while the layouts load', () => {
  afterEach(() => {
    cleanup();
  });

  it('draws the sidebar, the top bar, the loading page and the content slot', async () => {
    const { container } = await renderAt('/o/acme/f/build/agents');

    expect(screen.getByRole('navigation', { name: 'Organization and factories' })).toBeTruthy();
    expect(screen.getAllByRole('banner').length).toBeGreaterThan(0);
    expect(screen.getAllByText('acme').length).toBeGreaterThan(0);
    expect(screen.getByText('Loading')).toBeTruthy();
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    expect(container.querySelector('main#main')).not.toBeNull();
  });
});
