import { X } from 'lucide-react';
import { Dialog as D } from 'radix-ui';
import { type ReactNode, type RefObject, useRef } from 'react';
import { cn } from './cn';

export const Dialog = D.Root;
export const DialogTrigger = D.Trigger;
export const DialogClose = D.Close;

interface ContentProps {
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  className?: string;
  /** What takes focus on open (a field, the safe choice); otherwise Radix's first focusable, the close button. */
  initialFocus?: RefObject<HTMLElement | null>;
  /** Called as the dialog closes; preventDefault keeps focus from returning to what opened it. */
  onCloseAutoFocus?: (e: Event) => void;
  /**
   * false: only the dialog's own buttons close it (no close button, Escape or click outside), for
   * a dialog whose content is lost on closing, such as a secret shown once.
   */
  dismissible?: boolean;
}

/**
 * A modal with a required title. Radix traps focus; on close focus returns to the element that
 * had it when the dialog opened (Radix alone returns it only to a Dialog.Trigger, and most
 * dialogs open from plain buttons: WCAG 2.4.3), when that element is still on the page.
 */
export function DialogContent({
  title,
  description,
  children,
  className,
  initialFocus,
  onCloseAutoFocus,
  dismissible = true,
}: ContentProps) {
  const keep = (e: Event) => {
    if (!dismissible) e.preventDefault();
  };
  const opener = useRef<HTMLElement | null>(null);
  return (
    <D.Portal>
      <D.Overlay className="fixed inset-0 z-40 bg-ink/45 data-[state=open]:animate-[fade-in_150ms_ease-out]" />
      <D.Content
        onCloseAutoFocus={(e) => {
          onCloseAutoFocus?.(e);
          const back = opener.current;
          opener.current = null;
          if (e.defaultPrevented || !back?.isConnected) return;
          e.preventDefault();
          back.focus();
        }}
        className={cn(
          'fixed z-50 flex flex-col gap-4 bg-card p-5 shadow-dialog outline-none',
          'top-1/2 left-1/2 max-h-[calc(100dvh-32px)] w-[calc(100vw-32px)] max-w-[480px] -translate-x-1/2 -translate-y-1/2 overflow-y-auto overscroll-contain rounded-card',
          className,
        )}
        onEscapeKeyDown={keep}
        onPointerDownOutside={keep}
        onOpenAutoFocus={(e) => {
          // Focus is still where the dialog was opened from.
          opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
          if (!initialFocus?.current) return;
          e.preventDefault();
          initialFocus.current.focus();
        }}
        // Radix warns about a missing description unless aria-describedby is explicitly unset.
        {...(description ? {} : ({ 'aria-describedby': undefined } as object))}
      >
        <div className="flex items-start justify-between gap-4">
          <div className="flex min-w-0 flex-col gap-1">
            <D.Title className="m-0 text-lg font-medium">{title}</D.Title>
            {description ? <D.Description className="m-0 text-sm text-muted">{description}</D.Description> : null}
          </div>
          {dismissible ? (
            <D.Close
              className="-mt-1 -mr-1 inline-flex size-10 shrink-0 items-center justify-center rounded-control text-ink transition-colors hover:bg-nav-hover"
              aria-label="Close"
            >
              <X size={18} strokeWidth={1.5} aria-hidden="true" />
            </D.Close>
          ) : null}
        </div>
        {children}
      </D.Content>
    </D.Portal>
  );
}
