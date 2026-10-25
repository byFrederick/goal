import { X } from 'lucide-react';
import { Dialog as D } from 'radix-ui';
import { type ReactNode, useRef } from 'react';

export const Drawer = D.Root;
export const DrawerTrigger = D.Trigger;

interface DrawerContentProps {
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
}

/**
 * A right-side drawer (Radix Dialog): about 460 px on a desktop, a full-screen sheet below 768 px.
 * The page behind is inert, focus is trapped and Escape closes it. On close focus returns to the
 * element that opened it when that element is still on the page (as DialogContent does), so a
 * board that re-renders under the drawer does not strand focus on the body (WCAG 2.4.3).
 */
export function DrawerContent({ title, description, children }: DrawerContentProps) {
  const opener = useRef<HTMLElement | null>(null);
  return (
    <D.Portal>
      <D.Overlay className="fixed inset-0 z-40 bg-ink/45 max-md:hidden data-[state=open]:animate-[fade-in_150ms_ease-out]" />
      <D.Content
        onOpenAutoFocus={() => {
          opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        }}
        onCloseAutoFocus={(e) => {
          const back = opener.current;
          opener.current = null;
          if (!back?.isConnected) return;
          e.preventDefault();
          back.focus();
        }}
        className="fixed inset-0 z-50 flex flex-col gap-4 overflow-y-auto overscroll-contain bg-card p-5 shadow-dialog outline-none data-[state=open]:animate-[fade-in_150ms_ease-out] md:inset-y-0 md:right-0 md:left-auto md:w-[460px] md:border-l md:border-line"
        // Radix warns about a missing description unless aria-describedby is explicitly unset.
        {...(description ? {} : ({ 'aria-describedby': undefined } as object))}
      >
        <div className="flex items-start justify-between gap-4">
          <div className="flex min-w-0 flex-col gap-1">
            <D.Title className="m-0 text-lg font-medium">{title}</D.Title>
            {description ? <D.Description className="m-0 text-sm text-muted">{description}</D.Description> : null}
          </div>
          <D.Close
            className="-mt-1 -mr-1 inline-flex size-10 shrink-0 items-center justify-center rounded-control text-ink transition-colors hover:bg-nav-hover"
            aria-label="Close"
          >
            <X size={18} strokeWidth={1.5} aria-hidden="true" />
          </D.Close>
        </div>
        {children}
      </D.Content>
    </D.Portal>
  );
}
