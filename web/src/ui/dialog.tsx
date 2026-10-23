import { X } from 'lucide-react';
import { Dialog as D } from 'radix-ui';
import type { ReactNode } from 'react';
import { cn } from './cn';

export const Dialog = D.Root;
export const DialogTrigger = D.Trigger;
export const DialogClose = D.Close;

interface ContentProps {
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  /** `sheet` docks to the bottom on phones, for menus. */
  variant?: 'dialog' | 'sheet';
  className?: string;
}

/** A modal with a required title (Radix traps focus and restores it on close). */
export function DialogContent({ title, description, children, variant = 'dialog', className }: ContentProps) {
  return (
    <D.Portal>
      <D.Overlay className="fixed inset-0 z-40 bg-ink/45 data-[state=open]:animate-[fade-in_150ms_ease-out]" />
      <D.Content
        className={cn(
          'fixed z-50 flex flex-col gap-4 bg-card p-5 shadow-dialog outline-none',
          variant === 'dialog' &&
            'top-1/2 left-1/2 max-h-[calc(100dvh-32px)] w-[calc(100vw-32px)] max-w-[480px] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-card',
          variant === 'sheet' &&
            'inset-x-0 bottom-0 max-h-[85dvh] overflow-y-auto rounded-t-card pb-[max(20px,env(safe-area-inset-bottom))] md:top-1/2 md:right-auto md:bottom-auto md:left-1/2 md:w-[420px] md:-translate-x-1/2 md:-translate-y-1/2 md:rounded-card',
          className,
        )}
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
