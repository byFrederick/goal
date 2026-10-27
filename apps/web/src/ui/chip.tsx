import type { ReactNode } from 'react';
import { cn } from './cn';

// MASTER.md, Components: chips are 22 px (28 px large) with the chip radius, a tint fill and the
// tone's ink text. Neutral chips are panel-coloured with a line border. A status dot takes the
// tone's colour; only the running dot pulses, and reduced motion turns the pulse off.
const tones = {
  plain: { chip: 'border-line bg-panel text-ink', dot: 'bg-muted' },
  quiet: { chip: 'border-line bg-panel text-ink', dot: 'bg-muted' },
  run: {
    chip: 'border-transparent bg-run-tint text-run-ink',
    dot: 'bg-run animate-pulse-dot motion-reduce:animate-none',
  },
  you: { chip: 'border-transparent bg-you-tint text-you-ink', dot: 'bg-you' },
  rework: { chip: 'border-transparent bg-rework-tint text-rework-ink', dot: 'bg-rework' },
  /** The constraint (the drum): yellow. */
  constraint: { chip: 'border-transparent bg-amber-tint text-amber-ink', dot: 'bg-amber' },
  /** Expedite lane: yellow, like the constraint, so the two read as one kind of urgency. */
  expedite: { chip: 'border-transparent bg-amber-tint text-amber-ink', dot: 'bg-amber' },
  ok: { chip: 'border-transparent bg-ok-tint text-ok-ink', dot: 'bg-ok' },
} as const;

export type ChipTone = keyof typeof tones;

const sizes = {
  md: 'h-5.5 px-2 text-xs',
  lg: 'h-7 px-3 text-secondary',
} as const;

/** A label: work type, lane, state. Never colour alone: the text says it. */
export function Chip({
  tone = 'plain',
  size = 'md',
  dot = false,
  className,
  children,
}: {
  tone?: ChipTone;
  size?: keyof typeof sizes;
  /** Draws a status dot before the text, in the tone's colour. */
  dot?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-chip border font-control whitespace-nowrap',
        sizes[size],
        tones[tone].chip,
        className,
      )}
    >
      {dot ? <Dot className={tones[tone].dot} /> : null}
      {children}
    </span>
  );
}

/** A status dot; decorative, always next to text that says the same. */
export function Dot({ className }: { className?: string }) {
  return <span aria-hidden="true" className={cn('inline-block size-2 shrink-0 rounded-full', className)} />;
}
