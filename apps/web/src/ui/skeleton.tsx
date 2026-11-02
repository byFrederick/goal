// A placeholder that waits in the slot of the content it stands for (MOTION.md, Loading): the
// placeholder pulses once, then both layers cross-fade with a 2 px blur to the content. The two layers
// share one grid cell, so the swap moves nothing. While it waits, the wrapper is aria-busy and says
// "Loading" to a screen reader; the content is inert, so nothing in it takes focus before it shows.
import type { ReactNode } from 'react';
import { cn } from './cn';

interface SkeletonProps {
  /** False while the data loads: the placeholder shows. True: the content replaces it. */
  ready: boolean;
  /** The real layout drawn as muted blocks. It pulses once, then fades out. */
  placeholder: ReactNode;
  children: ReactNode;
  className?: string;
}

export function Skeleton({ ready, placeholder, children, className }: SkeletonProps) {
  return (
    <div aria-busy={ready ? undefined : true} className={cn('grid', className)}>
      {ready ? null : <span className="sr-only">Loading</span>}
      <div
        aria-hidden="true"
        className={cn(
          'col-start-1 row-start-1 transition-[opacity,filter] duration-(--duration-slow) ease-in-out',
          ready && 'opacity-0 blur-(--blur-small)',
        )}
      >
        <div className="animate-skeleton-pulse">{placeholder}</div>
      </div>
      <div
        inert={!ready}
        className={cn(
          'col-start-1 row-start-1 transition-[opacity,filter] duration-(--duration-slow) ease-in-out',
          !ready && 'opacity-0 blur-(--blur-small)',
        )}
      >
        {children}
      </div>
    </div>
  );
}
