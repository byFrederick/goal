// A row that leaves a list once its action is answered (MOTION.md, A rule leaves a list after Revoke): its
// opacity goes to 0 and its height collapses, then the list refreshes. Under reduced motion the row is gone
// at once. `useLeaving` runs `refresh` after the exit, or at once when the row unmounts first.
import { type ReactNode, useEffect, useRef, useState } from 'react';
import { cn } from './cn';

/** Mirrors --duration-quick in styles.css, the exit's length (tooltip.tsx does the same for its delay). */
export const LEAVE_MS = 150;

export function useLeaving(refresh: () => void): { leaving: boolean; leave: () => void } {
  const [leaving, setLeaving] = useState(false);
  const pending = useRef<(() => void) | undefined>(undefined);
  useEffect(() => () => pending.current?.(), []);
  const leave = () => {
    setLeaving(true);
    pending.current = () => {
      pending.current = undefined;
      refresh();
    };
    const reduced =
      typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    setTimeout(() => pending.current?.(), reduced ? 0 : LEAVE_MS);
  };
  return { leaving, leave };
}

export function Leaving({
  leaving,
  className,
  children,
}: {
  leaving: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div
      data-leaving={leaving ? '' : undefined}
      className={cn(
        'grid grid-rows-[1fr] transition-[grid-template-rows,opacity] duration-(--duration-quick) ease-out motion-reduce:transition-none',
        leaving && 'grid-rows-[0fr] opacity-0',
        className,
      )}
    >
      <div className="min-h-0 overflow-hidden">{children}</div>
    </div>
  );
}
