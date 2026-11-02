import type { QueryClient } from '@tanstack/react-query';
import { createRootRouteWithContext, HeadContent, Outlet } from '@tanstack/react-router';
import { pageTitle, useRouteFocus } from '../app/route-focus';
import { ReauthHost } from '../shared/reauth';
import { useTheme } from '../shared/theme';
import { Toaster } from '../ui/toaster';
import { TooltipProvider } from '../ui/tooltip';

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  head: () => pageTitle(),
  component: Root,
});

function Root() {
  useRouteFocus();
  const { resolved } = useTheme();
  return (
    <TooltipProvider>
      {/* The current route's <title>; React hoists it into <head>. */}
      <HeadContent />
      <Outlet />
      <ReauthHost />
      <Toaster theme={resolved} />
    </TooltipProvider>
  );
}
