// @vitest-environment jsdom
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NotFoundScreen } from './states';

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
