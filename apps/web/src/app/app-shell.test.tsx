// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { OrgSummary } from '../api/types';
import { OrgContext } from '../shared/org';
import { ShellFrame } from './app-shell';

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 1, role: 'owner' };

describe('ShellFrame', () => {
  afterEach(cleanup);

  it('clips the column sideways, so an edge button’s hit area never widens the page', async () => {
    const root = createRootRoute();
    const page = createRoute({
      getParentRoute: () => root,
      path: '/',
      component: () => (
        <QueryClientProvider client={new QueryClient()}>
          <OrgContext value={org}>
            <ShellFrame>
              <p>Body</p>
            </ShellFrame>
          </OrgContext>
        </QueryClientProvider>
      ),
    });
    const router = createRouter({
      routeTree: root.addChildren([page]),
      history: createMemoryHistory({ initialEntries: ['/'] }),
    });
    const { container } = render(<RouterProvider router={router} />);
    await screen.findByText('Body');

    const column = container.querySelector('main#main')?.parentElement;
    expect(column?.className).toContain('overflow-x-clip');
    expect(column?.className).not.toContain('overflow-hidden');
  });
});
