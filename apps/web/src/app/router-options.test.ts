import { describe, expect, it } from 'vitest';
import { routerOptions } from './router-options';
import { PendingScreen } from './states';

describe('the router defaults', () => {
  it('shows the skeleton after 100 ms and keeps it for at least 300 ms', () => {
    expect(routerOptions.defaultPendingMs).toBe(100);
    expect(routerOptions.defaultPendingMinMs).toBe(300);
  });

  it('animates route changes with the View Transitions API', () => {
    expect(routerOptions.defaultViewTransition).toEqual({ types: ['route'] });
  });

  it('shows the page skeleton as the pending component of every route', () => {
    expect(routerOptions.defaultPendingComponent).toBe(PendingScreen);
  });
});
