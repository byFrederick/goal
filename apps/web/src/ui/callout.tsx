import { Check, Info, TriangleAlert } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from './cn';

const tones = {
  /** What went wrong and what to do next. */
  rework: { box: 'border-rework-tint bg-rework-tint px-3 py-2.5 text-rework-ink', Icon: TriangleAlert },
  /** What just worked ("We sent a new code."). */
  ok: { box: 'border-ok/25 bg-ok-tint px-3 py-2.5 text-ok-ink', Icon: Check },
  /** A plain note beside a form, without a box. */
  note: { box: 'border-transparent text-ink-soft', Icon: Info },
} as const;

/**
 * A message next to what it is about: an icon and one or two sentences. Put it in a live region
 * (aria-live) when it appears after an action, so screen readers say it.
 */
export function Callout({ tone, id, children }: { tone: keyof typeof tones; id?: string; children: ReactNode }) {
  const { box, Icon } = tones[tone];
  return (
    <p id={id} className={cn('m-0 flex items-start gap-2.5 rounded-control border text-sm leading-snug', box)}>
      <Icon
        size={16}
        strokeWidth={1.75}
        aria-hidden="true"
        className={cn('mt-0.5 shrink-0', tone === 'note' && 'text-muted')}
      />
      <span>{children}</span>
    </p>
  );
}
