import type { QueryClient } from '@tanstack/react-query';
import { createRootRouteWithContext, HeadContent, Outlet } from '@tanstack/react-router';
import { Toaster } from 'sonner';
import { pageTitle, useRouteFocus } from '../app/route-focus';
import { ReauthHost } from '../shared/reauth';
import { useTheme } from '../shared/theme';
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
      <Toaster
        theme={resolved}
        position="top-center"
        toastOptions={{
          classNames: {
            toast: '!rounded-card !border-line !bg-card !font-sans !text-ink !shadow-pop',
            description: '!text-muted',
            // The update prompt's Reload: the primary button of the design system (ink on white).
            actionButton: '!rounded-control !bg-ink !font-sans !text-white hover:!bg-ink/90',
            success: '[&_[data-icon]]:!text-ok',
            error: '[&_[data-icon]]:!text-rework',
          },
        }}
      />
    </TooltipProvider>
  );
}
