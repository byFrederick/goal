import { cn } from './cn';

/** A busy button's 16 px spinner: white on ink, ink on light. Decorative: the label says it. */
export function Spinner({ tone = 'light', className }: { tone?: 'light' | 'dark'; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'size-4 shrink-0 animate-spin rounded-full border-2',
        tone === 'light' ? 'border-white/35 border-t-white' : 'border-ink/20 border-t-ink',
        className,
      )}
    />
  );
}
