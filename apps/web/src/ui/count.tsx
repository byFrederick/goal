import { cn } from './cn';
import { NumberRoll } from './number-roll';

const tones = {
  /** A plain tally (factories, proposals): concrete, ink-soft on the active nav item. */
  neutral: 'bg-panel text-ink group-aria-[current=page]:bg-ink-soft group-aria-[current=page]:text-white',
  /** What waits on you: violet. */
  you: 'bg-you text-white',
} as const;

/**
 * A small count at the end of a nav entry. `label` finishes the sentence for screen readers
 * (" waiting on you"), so the number never stands alone. Put it inside the link with `group`.
 * The badge slides in and pops when it appears (MOTION.md, notification-badge); the number rolls through
 * NumberRoll when it changes. Reduced motion keeps the fade only.
 */
export function Count({ value, label, tone = 'neutral' }: { value: number; label: string; tone?: keyof typeof tones }) {
  return (
    <span
      className={cn(
        'ml-auto inline-flex h-[22px] min-w-[22px] items-center justify-center rounded-chip px-1.5 text-xs font-semibold tabular-nums motion-safe:animate-notification-badge motion-reduce:animate-fade-in',
        tones[tone],
      )}
    >
      <NumberRoll value={value} />
      <span className="sr-only">{label}</span>
    </span>
  );
}
