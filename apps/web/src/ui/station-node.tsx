import { Check } from 'lucide-react';
import { cn } from './cn';

/**
 * A station on the line (MASTER.md, Components: the andon panel). A 54 px node whose number is the
 * items at the station. The constraint has a yellow ring; Done (`done`) shows a check instead.
 * Home's mini line draws the same node at 24 px (`compact`): the same tones, a smaller drawing.
 */
export type StationNodeTone = 'plain' | 'constraint' | 'done';
export type StationNodeSize = 'andon' | 'compact';

const tones: Record<StationNodeTone, string> = {
  plain: 'border-run-soft',
  constraint: 'border-amber ring-6 ring-amber/20',
  done: 'border-ok',
};

const sizes: Record<StationNodeSize, string> = {
  andon: 'size-13.5 border-3 text-heading',
  compact: 'size-6 border-2 text-meta',
};

export function StationNode({
  tone = 'plain',
  size = 'andon',
  count,
  className,
}: {
  tone?: StationNodeTone;
  size?: StationNodeSize;
  /** The items at the station. Omitted for Done, which shows its check. */
  count?: number | undefined;
  className?: string | undefined;
}) {
  const compact = size === 'compact';
  return (
    <span
      className={cn(
        'relative z-10 grid shrink-0 place-items-center rounded-full bg-andon-raised font-numeral text-white tabular-nums',
        sizes[size],
        tones[tone],
        className,
      )}
    >
      {tone === 'done' ? (
        <Check size={compact ? 12 : 20} strokeWidth={2.2} className="text-ok" aria-hidden="true" />
      ) : (
        count
      )}
    </span>
  );
}
