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
import { BoardShape, FloorShape, ItemShape, ListShape, PageShape } from './route-skeletons';
import { ShellPending, shapeFor } from './shell-pending';

async function renderAt(path: string, route = '/o/$org/f/$factory/agents') {
  const root = createRootRoute();
  const page = createRoute({
    getParentRoute: () => root,
    path: route,
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

  it('draws the phone tab bar placeholder with the five tabs and no crumb row in the phone top bar', async () => {
    const { container } = await renderAt('/o/acme/f/build/board', '/o/$org/f/$factory/board');

    const tabs = screen.getByRole('navigation', { name: 'Tabs' });
    expect(tabs.textContent).toBe('Needs youFloorBoardItemsMore');
    expect(tabs.className).toContain('md:hidden');
    const phoneBar = container.querySelector('header.md\\:hidden');
    expect(phoneBar?.querySelector('nav[aria-label="Breadcrumb"]')).toBeNull();
  });

  it('draws the Board shape, not the generic page, for a Board URL', async () => {
    const board = await renderAt('/o/acme/f/build/board', '/o/$org/f/$factory/board');
    const boardBlocks = board.container.querySelectorAll('.animate-skeleton-pulse *').length;
    cleanup();
    const other = await renderAt('/o/acme/f/build/unknown', '/o/$org/f/$factory/unknown');
    const pageBlocks = other.container.querySelectorAll('.animate-skeleton-pulse *').length;
    expect(boardBlocks).toBeGreaterThan(pageBlocks);
    expect(shapeFor('/o/acme/f/build/board')).toEqual(<BoardShape />);
    expect(shapeFor('/o/acme/f/build/items/12')).toEqual(<ItemShape />);
    expect(shapeFor('/o/acme/runs')).toEqual(<ListShape />);
    expect(shapeFor('/o/acme/f/build')).toEqual(<FloorShape />);
    expect(shapeFor('/o/acme/f/build/other')).toEqual(<PageShape />);
  });
});
