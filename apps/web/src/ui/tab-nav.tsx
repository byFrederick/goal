// A row of page links drawn as one pill, the navigation twin of SegmentedControl (MOTION.md, Tabs and
// segmented controls). The link with aria-current="page" sits on one pill that slides to the new current
// link. The pill is measured from the links, so it is placed before the first paint without a slide, and
// it slides only when the current link changes.
import { type ReactNode, useLayoutEffect, useRef } from 'react';
import { cn } from './cn';

/** The class of each link or button inside a TabNav. */
export const tabNavItem =
  'press relative inline-flex h-11 shrink-0 items-center gap-1 rounded-chip px-3 text-secondary whitespace-nowrap text-muted hover:text-ink aria-[current=page]:font-control aria-[current=page]:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run md:h-7.5';

/** The class of each link inside a TabNav with variant="underline": a row on a rule, the current link underlined. */
export const tabNavUnderlineItem =
  'press relative inline-flex h-11 shrink-0 items-center gap-2 px-1 text-secondary whitespace-nowrap text-muted hover:text-ink aria-[current=page]:font-control aria-[current=page]:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run md:h-10';

export function TabNav({
  label,
  className,
  variant = 'pill',
  children,
}: {
  label: string;
  className?: string;
  /** "pill": one pill under the current link. "underline": a rule under the row and a 2px ink line under the current link. */
  variant?: 'pill' | 'underline';
  children: ReactNode;
}) {
  const nav = useRef<HTMLElement>(null);
  const pill = useRef<HTMLSpanElement>(null);
  const placed = useRef(false);
  // A link that a key activated moves the indicator at once: keyboard use never animates (MOTION.md rule 1).
  const byKeyboard = useRef(false);
  const underline = variant === 'underline';

  // Runs after every render: the current link, the counts or the width may have changed.
  useLayoutEffect(() => {
    const place = (animate: boolean) => {
      const el = pill.current;
      if (!el) return;
      const current = nav.current?.querySelector<HTMLElement>('[aria-current="page"]');
      if (!current) {
        el.style.opacity = '0';
        return;
      }
      el.style.transition = animate ? '' : 'none';
      el.style.opacity = '1';
      el.style.width = `${current.offsetWidth}px`;
      el.style.transform = `translateX(${current.offsetLeft}px)`;
      // The first paint and a window resize restore the transition; a keyboard change keeps it off until the next click.
      if (!animate && !byKeyboard.current) {
        void el.offsetWidth;
        el.style.transition = '';
      }
    };
    place(placed.current && !byKeyboard.current);
    placed.current = true;
    const onResize = () => place(false);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  });

  return (
    <nav
      ref={nav}
      aria-label={label}
      onKeyDownCapture={() => {
        byKeyboard.current = true;
      }}
      onPointerDownCapture={() => {
        byKeyboard.current = false;
      }}
      className={cn(
        'relative flex max-w-full overflow-x-auto',
        underline
          ? 'gap-5 self-stretch border-b border-line'
          : 'inline-flex gap-px self-start rounded-control border border-line bg-panel p-1',
        className,
      )}
    >
      <span
        ref={pill}
        aria-hidden="true"
        className={cn(
          'pointer-events-none absolute left-0 opacity-0 transition-[transform,width] duration-(--duration-fast) ease-out',
          underline ? 'bottom-0 h-0.5 bg-ink' : 'inset-y-1 rounded-chip bg-card shadow-raised',
        )}
      />
      {children}
    </nav>
  );
}
