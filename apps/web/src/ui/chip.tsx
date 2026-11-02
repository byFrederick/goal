import { type ReactNode, useState } from 'react';
import { cn } from './cn';

// MASTER.md, Components: chips are 22 px (28 px large) with the chip radius, white (panel) with a line
// border, and a 6 px status dot in the tone's colour. Only "needs you" and "sent back" take a tint fill,
// because they ask for action. Only the running dot pulses, and reduced motion turns the pulse off.
// When the text changes in place, the new text rises in (MOTION.md, A status changes in place).
const tones = {
  plain: { chip: 'border-line bg-panel text-ink', dot: 'bg-muted' },
  quiet: { chip: 'border-line bg-panel text-ink', dot: 'bg-muted' },
  run: {
    chip: 'border-line bg-panel text-ink',
    dot: 'bg-run animate-pulse-dot motion-reduce:animate-none',
  },
  you: { chip: 'border-transparent bg-you-tint text-you-ink', dot: 'bg-you' },
  rework: { chip: 'border-transparent bg-rework-tint text-rework-ink', dot: 'bg-rework' },
  /** The constraint (the drum): yellow. */
  constraint: { chip: 'border-line bg-panel text-ink', dot: 'bg-amber' },
  /** Expedite lane: yellow, like the constraint, so the two read as one kind of urgency. */
  expedite: { chip: 'border-line bg-panel text-ink', dot: 'bg-amber' },
  ok: { chip: 'border-line bg-panel text-ink', dot: 'bg-ok' },
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
      {dot ? <Dot className={cn('size-1.5', tones[tone].dot)} /> : null}
      {typeof children === 'string' || typeof children === 'number' ? <ChipText text={String(children)} /> : children}
    </span>
  );
}

/**
 * The chip's text. When it changes in place, the new text is drawn again with the swap animation; the
 * first draw does not animate, so a list of chips does not move when it first appears.
 */
function ChipText({ text }: { text: string }) {
  const [seen, setSeen] = useState({ text, generation: 0 });
  let { generation } = seen;
  if (seen.text !== text) {
    generation += 1;
    setSeen({ text, generation });
  }
  return (
    <span
      key={generation}
      className={cn(generation > 0 && 'motion-safe:animate-text-states-swap motion-reduce:animate-fade-in')}
    >
      {text}
    </span>
  );
}

/** A status dot; decorative, always next to text that says the same. */
export function Dot({ className }: { className?: string }) {
  return <span aria-hidden="true" className={cn('inline-block size-2 shrink-0 rounded-full', className)} />;
}
