import { cn } from './cn';
import { NumberRoll } from './number-roll';

/**
 * A count at the end of a nav entry or a title: an orange pill (`--color-you`) with a white number,
 * 18 px high. Every count is a person's queue (what waits on you or for your review). `label`
 * finishes the sentence for screen readers (" waiting on you"), so the number never stands alone.
 * The badge slides in and pops when it appears (MOTION.md, notification-badge); the number rolls through
 * NumberRoll when it changes. Reduced motion keeps the fade only.
 */
export function Count({ value, label }: { value: number; label: string }) {
  return (
    <span
      className={cn(
        'ml-auto inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-full bg-you px-[5px] text-meta font-semibold tabular-nums text-white motion-safe:animate-notification-badge motion-reduce:animate-fade-in',
      )}
    >
      <NumberRoll value={value} />
      <span className="sr-only">{label}</span>
    </span>
  );
}
