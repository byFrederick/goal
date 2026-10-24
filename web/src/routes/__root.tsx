import type { QueryClient } from '@tanstack/react-query';
import { createRootRouteWithContext, HeadContent, Outlet } from '@tanstack/react-router';
import { Toaster } from 'sonner';
import { pageTitle, useRouteFocus } from '../app/route-focus';
import { TooltipProvider } from '../ui/tooltip';

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  head: () => pageTitle(),
  component: Root,
});

function Root() {
  useRouteFocus();
  return (
    <TooltipProvider>
      {/* The current route's <title>; React hoists it into <head>. */}
      <HeadContent />
      <Outlet />
      <Toaster
        position="top-center"
        toastOptions={{
          classNames: {
            toast: '!rounded-card !border-line !bg-card !font-sans !text-ink !shadow-dialog',
            description: '!text-muted',
            success: '[&_[data-icon]]:!text-ok',
            error: '[&_[data-icon]]:!text-rework',
          },
        }}
      />
    </TooltipProvider>
  );
}
