import { cn } from './cn';

const tones = {
  /** A plain tally (factories, proposals): concrete, ink-soft on the active nav item. */
  neutral: 'bg-control text-ink group-aria-[current=page]:bg-ink-soft group-aria-[current=page]:text-white',
  /** What waits on you: violet. */
  you: 'bg-you text-white',
} as const;

/**
 * A small count at the end of a nav entry. `label` finishes the sentence for screen readers
 * (" waiting on you"), so the number never stands alone. Put it inside the link with `group`.
 */
export function Count({ value, label, tone = 'neutral' }: { value: number; label: string; tone?: keyof typeof tones }) {
  return (
    <span
      className={cn(
        'ml-auto inline-flex h-[22px] min-w-[22px] items-center justify-center rounded-chip px-1.5 text-xs font-semibold tabular-nums',
        tones[tone],
      )}
    >
      {value}
      <span className="sr-only">{label}</span>
    </span>
  );
}
