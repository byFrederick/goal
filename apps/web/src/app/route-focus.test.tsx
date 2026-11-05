// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useRouteFocus } from './route-focus';

type Handler = (event: { fromLocation?: unknown; pathChanged?: boolean }) => void;
const handlers = new Map<string, Handler>();

vi.mock('@tanstack/react-router', () => ({
  useRouter: () => ({
    subscribe: (name: string, handler: Handler) => {
      handlers.set(name, handler);
      return () => handlers.delete(name);
    },
  }),
}));

function Probe() {
  useRouteFocus();
  return null;
}

/** The page's main region. */
const main = () => document.querySelector('#main') as HTMLElement;

/** One animation frame, as the hook waits for after the router resolves. */
const frame = () => new Promise((resolve) => requestAnimationFrame(() => resolve(undefined)));

beforeEach(() => {
  handlers.clear();
  document.body.innerHTML = '<main id="main"><h1>Floor</h1></main>';
  render(<Probe />);
});

afterEach(() => {
  cleanup();
  document.body.innerHTML = '';
});

describe('focus after navigation', () => {
  it('focuses the new page heading when the page has already replaced the old one', async () => {
    handlers.get('onBeforeNavigate')?.({});
    main().innerHTML = '<h1>Home</h1>';
    handlers.get('onResolved')?.({ fromLocation: {}, pathChanged: true });
    await frame();
    await frame();

    expect(document.activeElement?.textContent).toBe('Home');
  });

  it('waits for the new heading when the router resolves while the old page is still drawn', async () => {
    handlers.get('onBeforeNavigate')?.({});
    handlers.get('onResolved')?.({ fromLocation: {}, pathChanged: true });
    await frame();
    await frame();
    expect(document.activeElement).toBe(document.body);

    main().innerHTML = '<h1>Organization settings</h1>';

    await vi.waitFor(() => expect(document.activeElement?.textContent).toBe('Organization settings'));
  });

  it('leaves focus alone when a search parameter changes and the path stays', async () => {
    handlers.get('onBeforeNavigate')?.({});
    handlers.get('onResolved')?.({ fromLocation: {}, pathChanged: false });
    await frame();
    await frame();

    expect(document.activeElement).toBe(document.body);
  });
});
