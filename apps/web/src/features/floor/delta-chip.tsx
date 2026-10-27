import { cn } from '../../ui/cn';
import type { Delta } from './deltas';

const tones = { worse: 'text-rework', better: 'text-ok-ink', flat: 'text-muted' } as const;

/**
 * A signed change with an arrow, rust when worse and green when better: a white chip from 768
 * px, plain text on phones. The arrow and sign carry the direction; screen readers also hear
 * whether it is better or worse.
 */
export function DeltaChip({ delta }: { delta: Delta }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 text-[13px] font-medium whitespace-nowrap tabular-nums md:h-[26px] md:rounded-chip md:bg-card md:px-2',
        tones[delta.tone],
      )}
    >
      {delta.direction === 'flat' ? null : (
        <svg aria-hidden="true" viewBox="0 0 10 10" className="size-2.5 max-md:hidden">
          <path d={delta.direction === 'up' ? 'M5 1.5 9 8.5H1Z' : 'M5 8.5 9 1.5H1Z'} fill="currentColor" />
        </svg>
      )}
      {delta.text}
      {delta.tone === 'flat' ? null : <span className="sr-only">, {delta.tone}</span>}
    </span>
  );
}
