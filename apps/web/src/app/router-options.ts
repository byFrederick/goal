// The router's defaults, in one object so a test can check them and main.tsx only wires the client.
// Pending: a route's data that takes longer than 100 ms shows its skeleton, and that skeleton stays for
// at least 300 ms, so a fast load does not flicker (MOTION.md, Loading). Route changes use the View
// Transitions API where the browser has it (MOTION.md, Route change; the fade lives in styles.css).
import { createElement } from 'react';
import { ErrorScreen, NotFoundScreen, PendingScreen } from './states';

export const routerOptions = {
  defaultPreload: 'intent',
  // Queries own freshness; the router should always ask them.
  defaultPreloadStaleTime: 0,
  defaultPendingComponent: PendingScreen,
  defaultPendingMs: 100,
  defaultPendingMinMs: 300,
  defaultViewTransition: true,
  defaultErrorComponent: ErrorScreen,
  defaultNotFoundComponent: () => createElement(NotFoundScreen),
  scrollRestoration: true,
} as const;
