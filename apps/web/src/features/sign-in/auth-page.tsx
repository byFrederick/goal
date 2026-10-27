// The frame of the pages before the app (canvas "Sign-In", "Mobile-Sign-In"): no sidebar, one card
// centered on the app's background; on phones the card's content fills the width.
import type { ReactNode, Ref } from 'react';
import { cn } from '../../ui/cn';
import { Wordmark } from '../../ui/logo';

/** wide: the card of a page with more to read (Device-Approve, 540 px). */
export function AuthPage({ wide = false, children }: { wide?: boolean; children: ReactNode }) {
  return (
    <main
      id="main"
      className="flex min-h-dvh flex-col items-center bg-shell px-5 pt-14 pb-12 sm:justify-center sm:px-4 sm:py-12"
    >
      <div
        className={cn(
          'flex w-full flex-col gap-5 sm:rounded-card sm:border sm:border-line sm:bg-card sm:p-8',
          wide ? 'max-w-[540px]' : 'max-w-[440px]',
        )}
      >
        <Wordmark />
        {children}
      </div>
    </main>
  );
}

/**
 * The page's one heading. A page whose content changes in place (Device-Approve's steps) moves the
 * focus to it, so `ref` takes it and it is focusable from script only.
 */
export function AuthHeading({ ref, children }: { ref?: Ref<HTMLHeadingElement>; children: ReactNode }) {
  return (
    <h1
      ref={ref}
      tabIndex={ref ? -1 : undefined}
      className="m-0 text-[32px] leading-[1.1] font-normal tracking-display outline-none"
    >
      {children}
    </h1>
  );
}

/**
 * Where a page's messages appear: always on the page, so screen readers read what appears in it
 * (aria-live). Empty, it takes back the gap of the column it sits in (14 px, the forms').
 */
export function LiveMessage({ children }: { children: ReactNode }) {
  return (
    <div aria-live="polite" className="empty:-mt-3.5">
      {children}
    </div>
  );
}
