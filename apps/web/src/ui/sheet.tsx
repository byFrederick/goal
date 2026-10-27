import { X } from 'lucide-react';
import { Dialog as D } from 'radix-ui';
import type { ReactNode, RefObject } from 'react';

export const Sheet = D.Root;
export const SheetTrigger = D.Trigger;

interface SheetContentProps {
  title: ReactNode;
  children: ReactNode;
  /** What takes focus when the sheet opens (the current entry); the first focusable otherwise. */
  initialFocus?: RefObject<HTMLElement | null>;
  /** Called as the sheet closes; preventDefault keeps Radix from returning focus to its trigger. */
  onCloseAutoFocus?: (e: Event) => void;
}

/**
 * A modal sheet docked to the bottom of a phone (Radix Dialog: the page behind is inert, focus
 * is trapped and returns to the trigger, Escape closes it). Used for the organization sheet and
 * the tab bar's More.
 */
export function SheetContent({ title, children, initialFocus, onCloseAutoFocus }: SheetContentProps) {
  return (
    <D.Portal>
      <D.Overlay className="fixed inset-0 z-40 bg-ink/50 data-[state=open]:animate-[fade-in_150ms_ease-out]" />
      <D.Content
        aria-describedby={undefined}
        onOpenAutoFocus={(e) => {
          if (!initialFocus?.current) return;
          e.preventDefault();
          initialFocus.current.focus();
        }}
        onCloseAutoFocus={onCloseAutoFocus}
        className="fixed inset-x-0 bottom-0 z-50 flex max-h-[85dvh] flex-col gap-1 overflow-y-auto rounded-t-[16px] bg-card px-3 pt-2 pb-[max(28px,env(safe-area-inset-bottom))] shadow-pop outline-none data-[state=open]:animate-[fade-in_150ms_ease-out]"
      >
        <span aria-hidden="true" className="mx-auto mb-1.5 block h-1 w-10 rounded-sm bg-rule" />
        <div className="flex items-center justify-between pl-2.5">
          <D.Title className="m-0 text-lg font-medium">{title}</D.Title>
          <D.Close
            aria-label="Close"
            className="inline-flex size-11 items-center justify-center rounded-control text-ink transition-colors duration-200 hover:bg-hover"
          >
            <X size={20} strokeWidth={1.5} aria-hidden="true" />
          </D.Close>
        </div>
        {children}
      </D.Content>
    </D.Portal>
  );
}

/** A group label inside a sheet ("Organizations", "Acme"). */
export function SheetLabel({ id, children }: { id: string; children: ReactNode }) {
  return (
    <p id={id} className="m-0 mx-2.5 mt-2.5 mb-1 text-[13px] font-medium text-muted">
      {children}
    </p>
  );
}
