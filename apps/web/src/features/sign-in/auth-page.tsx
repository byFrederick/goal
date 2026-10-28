// The frame of the pages before the app (canvas "Sign-In", "Mobile-Sign-In", "Device-Approve"): no
// sidebar. On a phone the page is white, with the Goal mark above the content and no card. From 640 px
// the page is the sidebar colour, with the Goal mark top left and one card under it; the sign-in
// pages' card opens with the line band (Intake to Done), which is decoration only.
import { Check } from 'lucide-react';
import type { ReactNode, Ref } from 'react';
import { cn } from '../../ui/cn';
import { GoalMark } from '../../ui/goal-mark';
import { Wordmark } from '../../ui/logo';

type Station = { name: string; slots: boolean[] } | { name: 'Done'; done: true };

/** The line of the sign-in card: where a person is on the way to a merged change. Decorative. */
const line: Station[] = [
  { name: 'Intake', slots: [] },
  { name: 'Spec', slots: [true, true, false] },
  { name: 'Build', slots: [true, true, true, false, false, false] },
  { name: 'Review', slots: [true, false, false] },
  { name: 'Done', done: true },
];

function SignInLine() {
  return (
    <div aria-hidden="true" className="hidden bg-andon px-7 pt-7 pb-5 sm:block">
      <div className="relative flex justify-between">
        <span className="absolute inset-x-9 top-4.5 h-0.75 bg-run" />
        {line.map((station) => {
          const done = 'done' in station;
          const queued = station.name === 'Intake';
          return (
            <div key={station.name} className="relative flex w-18 flex-col items-center gap-2">
              <span
                className={cn(
                  'flex size-9.5 items-center justify-center rounded-full border-2 bg-andon text-white',
                  queued && 'border-andon-text',
                  !queued && !done && 'border-run',
                  done && 'border-ok text-ok',
                )}
              >
                {done ? <Check size={16} strokeWidth={2.4} /> : null}
              </span>
              <span className="text-meta text-white">{station.name}</span>
              {'slots' in station && station.slots.length > 0 ? (
                <span className="flex gap-1">
                  {station.slots.map((on, i) => (
                    <span
                      // The slots are a fixed row: their position is their identity.
                      // biome-ignore lint/suspicious/noArrayIndexKey: a fixed row of squares
                      key={i}
                      className={cn('size-2 rounded-chip border', on ? 'border-run bg-run' : 'border-andon-text')}
                    />
                  ))}
                </span>
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/** wide: the card of a page with more to read (Device-Approve, 480 px). band: the sign-in line on top (the sign-in and code pages). */
export function AuthPage({
  wide = false,
  band = false,
  children,
}: {
  wide?: boolean;
  band?: boolean;
  children: ReactNode;
}) {
  return (
    <main id="main" className="flex min-h-dvh flex-col bg-shell px-6 pt-14 pb-7 sm:bg-sidebar sm:px-8 sm:pt-7 sm:pb-10">
      <div className="hidden sm:block">
        <Wordmark />
      </div>
      <div
        className={cn(
          'flex w-full flex-col sm:mx-auto sm:mt-14 sm:overflow-hidden sm:rounded-card sm:border sm:border-line sm:bg-card',
          wide ? 'sm:w-120' : 'sm:w-110',
        )}
      >
        {band ? <SignInLine /> : null}
        <div className="flex flex-col gap-6 sm:gap-4 sm:p-7">
          <GoalMark size="hero" className="sm:hidden" />
          {children}
        </div>
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
    <h1 ref={ref} tabIndex={ref ? -1 : undefined} className="m-0 text-page-title tracking-display outline-none">
      {children}
    </h1>
  );
}

/**
 * Where a page's messages appear: always on the page, so screen readers read what appears in it
 * (aria-live). Empty, it takes back the gap of the column it sits in (12 px, the forms').
 */
export function LiveMessage({ children }: { children: ReactNode }) {
  return (
    <div aria-live="polite" className="empty:-mt-3">
      {children}
    </div>
  );
}
