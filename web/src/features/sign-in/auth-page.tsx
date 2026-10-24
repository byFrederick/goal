// The frame of the pages before the app (canvas "Sign-In", "Mobile-Sign-In"): no rail, one card
// centered on the app's background; on phones the card's content fills the width.
import type { ReactNode } from 'react';
import { Wordmark } from '../../ui/logo';

export function AuthPage({ children }: { children: ReactNode }) {
  return (
    <main
      id="main"
      className="flex min-h-dvh flex-col items-center bg-shell px-5 pt-14 pb-12 sm:justify-center sm:px-4 sm:py-12"
    >
      <div className="flex w-full max-w-[440px] flex-col gap-5 sm:rounded-card sm:border sm:border-line sm:bg-card sm:p-8">
        <Wordmark />
        {children}
      </div>
    </main>
  );
}

/** The page's one heading. */
export function AuthHeading({ children }: { children: ReactNode }) {
  return <h1 className="m-0 text-[32px] leading-[1.1] font-normal tracking-display">{children}</h1>;
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
