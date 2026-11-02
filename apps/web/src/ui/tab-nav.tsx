// A row of page links drawn as one pill, the navigation twin of SegmentedControl (MOTION.md, Tabs and
// segmented controls). The link with aria-current="page" sits on one pill that slides to the new current
// link. The pill is measured from the links, so it is placed before the first paint without a slide, and
// it slides only when the current link changes.
import { type ReactNode, useLayoutEffect, useRef } from 'react';
import { cn } from './cn';

/** The class of each link or button inside a TabNav. */
export const tabNavItem =
  'press relative inline-flex h-11 shrink-0 items-center gap-1 rounded-chip px-3 text-secondary whitespace-nowrap text-muted hover:text-ink aria-[current=page]:font-control aria-[current=page]:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run md:h-7.5';

/** The class of each link inside an underline TabNav: a 40px row (44px on phones), the current link in ink. */
export const tabNavUnderlineItem =
  'press relative inline-flex h-11 shrink-0 items-center gap-2 px-3 text-secondary font-control whitespace-nowrap text-ink-soft transition-colors duration-(--duration-quick) hover:text-ink aria-[current=page]:font-semibold aria-[current=page]:text-ink focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-run md:h-10';

export function TabNav({
  label,
  className,
  variant = 'pill',
  children,
}: {
  label: string;
  className?: string;
  /** "pill" is the segmented look; "underline" is a rule under the row with a 2px ink indicator (Jonah). */
  variant?: 'pill' | 'underline';
  children: ReactNode;
}) {
  const underline = variant === 'underline';
  const nav = useRef<HTMLElement>(null);
  const pill = useRef<HTMLSpanElement>(null);
  const placed = useRef(false);

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
      if (!animate) el.style.transition = 'none';
      el.style.opacity = '1';
      el.style.width = `${current.offsetWidth}px`;
      el.style.transform = `translateX(${current.offsetLeft}px)`;
      if (!animate) {
        void el.offsetWidth;
        el.style.transition = '';
      }
    };
    place(placed.current);
    placed.current = true;
    const onResize = () => place(false);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  });

  return (
    <nav
      ref={nav}
      aria-label={label}
      className={cn(
        underline
          ? 'relative flex max-w-full gap-1 overflow-x-auto border-b border-rule'
          : 'relative inline-flex max-w-full gap-px self-start overflow-x-auto rounded-control border border-line bg-panel p-1',
        className,
      )}
    >
      <span
        ref={pill}
        aria-hidden="true"
        className={cn(
          'pointer-events-none absolute left-0 opacity-0 transition-[transform,width] duration-(--duration-fast) ease-out motion-reduce:transition-none',
          underline ? 'bottom-0 h-0.5 bg-ink' : 'inset-y-1 rounded-chip bg-card shadow-raised',
        )}
      />
      {children}
    </nav>
  );
}
