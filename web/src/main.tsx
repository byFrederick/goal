import './styles.css';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createRouter, RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ApiError } from './api/client';
import { safeNext, setSessionHandlers } from './api/session';
import { ErrorScreen, NotFoundScreen, PendingScreen } from './app/states';
import { routeTree } from './routeTree.gen';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      // A 4xx will not fix itself; a network blip might.
      retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
    },
  },
});

const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: 'intent',
  // Queries own freshness; the router should always ask them.
  defaultPreloadStaleTime: 0,
  defaultPendingComponent: PendingScreen,
  defaultPendingMs: 150,
  defaultErrorComponent: ErrorScreen,
  defaultNotFoundComponent: () => <NotFoundScreen />,
  scrollRestoration: true,
});

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

// goald answered 401: the session is gone (signed out elsewhere, expired, revoked). Nothing read
// with it is kept; the sign-in page opens and, once signed in, returns here (spec 3 §10.1).
setSessionHandlers({
  unauthenticated: () => {
    const here = router.state.location;
    if (here.pathname.startsWith('/sign-in')) return;
    queryClient.clear();
    const next = safeNext(here.href);
    void router.navigate({ to: '/sign-in', search: next ? { next } : {}, replace: true });
  },
});

const root = document.getElementById('root');
if (!root) throw new Error('index.html has no #root');

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
