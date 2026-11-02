// A keycap: the key a shortcut names, drawn as a small mono cap with a rule border (MASTER.md, keycaps).
import type { ReactNode } from 'react';
import { cn } from './cn';

/** The keycap's class, for a <kbd> that needs more around it. */
export const keyCap =
  'inline-grid h-5 min-w-5 place-items-center rounded-chip border border-rule px-1 font-mono text-meta font-semibold text-ink-soft';

export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return <kbd className={cn(keyCap, className)}>{children}</kbd>;
}
