import { cn } from './cn';

/** A busy button's 16 px spinner: action-ink on the action fill, ink on light. Decorative: the label says it. */
export function Spinner({ tone = 'light', className }: { tone?: 'light' | 'dark'; className?: string }) {
  return (
    <span
      aria-hidden="true"
      data-slot="spinner"
      className={cn(
        'size-4 shrink-0 animate-spin rounded-full border-2',
        tone === 'light' ? 'border-action-ink/35 border-t-action-ink' : 'border-ink/20 border-t-ink',
        className,
      )}
    />
  );
}
