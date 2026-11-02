import './styles.css';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createRouter, RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { keys } from './api/queries';
import { shouldRetry } from './api/retry';
import { safeNext, setSessionHandlers } from './api/session';
import { routerOptions } from './app/router-options';
import { registerServiceWorker } from './pwa/register';
import { routeTree } from './routeTree.gen';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      // A 4xx or a feature goald was built without will not fix itself; a network blip might (api/retry.ts).
      retry: shouldRetry,
    },
  },
});

const router = createRouter({
  routeTree,
  context: { queryClient },
  ...routerOptions,
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
  // A refusal means `can` was stale (a role changed): read it again, so the screens stop offering it.
  forbidden: () => {
    void queryClient.invalidateQueries({ queryKey: keys.orgs });
    void queryClient.invalidateQueries({
      predicate: ({ queryKey: [root, , what] }) => root === 'org' && (what === 'detail' || what === 'factories'),
    });
  },
});

registerServiceWorker();

const root = document.getElementById('root');
if (!root) throw new Error('index.html has no #root');

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
