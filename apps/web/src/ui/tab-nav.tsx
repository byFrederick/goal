// A row of page links drawn as one pill, the navigation twin of SegmentedControl (MOTION.md, Tabs and
// segmented controls). The link with aria-current="page" sits on one pill that slides to the new current
// link. The pill is measured from the links, so it is placed before the first paint without a slide, and
// it slides only when the current link changes.
import { type ReactNode, useLayoutEffect, useRef } from 'react';
import { cn } from './cn';

/** The class of each link or button inside a TabNav. */
export const tabNavItem =
  'press relative inline-flex h-11 shrink-0 items-center gap-1 rounded-chip px-3 text-secondary whitespace-nowrap text-muted hover:text-ink aria-[current=page]:font-control aria-[current=page]:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run md:h-7.5';

export function TabNav({ label, className, children }: { label: string; className?: string; children: ReactNode }) {
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
        'relative inline-flex max-w-full gap-px self-start overflow-x-auto rounded-control border border-line bg-panel p-1',
        className,
      )}
    >
      <span
        ref={pill}
        aria-hidden="true"
        className="pointer-events-none absolute inset-y-1 left-0 rounded-chip bg-card opacity-0 shadow-raised transition-[transform,width] duration-(--duration-fast) ease-out"
      />
      {children}
    </nav>
  );
}
