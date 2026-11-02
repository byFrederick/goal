import { Toaster as SonnerToaster, type ToasterProps } from 'sonner';

/**
 * The app's toasts (sonner), in the design system's toast style (MASTER.md, Components): bottom right,
 * an ink fill with white text (inverted in dark), an 8px radius and --shadow-pop. Their motion is in
 * styles.css (MOTION.md, Toast). Below 768 px they clear the phone's tab bar by --toaster-bottom.
 */
export function Toaster({ theme }: { theme: NonNullable<ToasterProps['theme']> }) {
  return (
    <SonnerToaster
      theme={theme}
      position="bottom-right"
      offset={{ bottom: 'var(--toaster-bottom)' }}
      mobileOffset={{ bottom: 'var(--toaster-bottom)' }}
      toastOptions={{
        classNames: {
          toast: '!rounded-card !border-0 !bg-action !font-sans !text-action-ink !shadow-pop',
          description: '!text-action-ink/70',
          // The toast's action: the inverse of the toast, so it reads as a button on the ink fill.
          actionButton: '!rounded-control !bg-action-ink !font-sans !text-action hover:!bg-action-ink/90',
          success: '[&_[data-icon]]:!text-ok',
          error: '[&_[data-icon]]:!text-rework',
        },
      }}
    />
  );
}
