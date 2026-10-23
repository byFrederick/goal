import type { QueryClient } from '@tanstack/react-query';
import { createRootRouteWithContext, Outlet } from '@tanstack/react-router';
import { Toaster } from 'sonner';

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: Root,
});

function Root() {
  return (
    <>
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
    </>
  );
}
