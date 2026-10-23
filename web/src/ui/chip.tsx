import type { ReactNode } from 'react';
import { cn } from './cn';

const tones = {
  plain: 'border-line bg-surface text-ink',
  quiet: 'border-transparent bg-shell text-ink',
  run: 'border-run-hatch bg-run-tint text-run',
  you: 'border-you-line bg-you-tint text-you',
  rework: 'border-rework-line bg-rework-tint text-rework-ink',
  constraint: 'border-amber bg-amber text-ink',
  ok: 'border-line bg-surface text-ok',
} as const;

export type ChipTone = keyof typeof tones;

/** A 6px-radius label: work type, lane, state. Never colour alone: the text says it. */
export function Chip({
  tone = 'plain',
  className,
  children,
}: {
  tone?: ChipTone;
  className?: string;
  children: ReactNode;
}) {
  return (
    <span
      className={cn(
        'inline-flex h-[22px] shrink-0 items-center gap-1.5 rounded-chip border px-[7px] text-xs font-medium whitespace-nowrap',
        tones[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}

/** A status dot; decorative, always next to text that says the same. */
export function Dot({ className }: { className?: string }) {
  return <span aria-hidden="true" className={cn('inline-block size-2 shrink-0 rounded-full', className)} />;
}
