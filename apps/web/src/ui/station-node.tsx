import { Check } from 'lucide-react';
import { cn } from './cn';

/**
 * A station on the line (MASTER.md, Components: the andon panel). A 54 px node whose number is the
 * items at the station. The constraint has a yellow ring; Done (`done`) shows a check instead.
 * Home's mini line draws the same node, so it lives here and takes no layout of its own.
 */
export type StationNodeTone = 'plain' | 'constraint' | 'done';

const tones: Record<StationNodeTone, string> = {
  plain: 'border-run-soft',
  constraint: 'border-amber ring-6 ring-amber/20',
  done: 'border-ok',
};

export function StationNode({
  tone = 'plain',
  count,
  className,
}: {
  tone?: StationNodeTone;
  /** The items at the station. Omitted for Done, which shows its check. */
  count?: number | undefined;
  className?: string | undefined;
}) {
  return (
    <span
      className={cn(
        'relative z-10 grid size-13.5 shrink-0 place-items-center rounded-full border-3 bg-andon-raised text-heading font-numeral text-white tabular-nums',
        tones[tone],
        className,
      )}
    >
      {tone === 'done' ? <Check size={20} strokeWidth={2.2} className="text-ok" aria-hidden="true" /> : count}
    </span>
  );
}
