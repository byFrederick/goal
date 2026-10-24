import type { ReactNode } from 'react';
import { cn } from './cn';

const kinds = {
  /** The organization: an ink tile with its two letters. */
  org: 'rounded-[12px] bg-ink font-semibold text-white',
  /** A factory: a white square, ink-filled when it is the current one. */
  factory: 'rounded-control border border-rule bg-surface font-medium text-ink group-hover:border-rule-strong',
  /** New factory: a dashed square; ink while the wizard is open. */
  add: 'rounded-control border border-dashed border-rule-strong bg-transparent text-ink-soft',
  /** An icon on the rail (catalog, credentials). */
  icon: 'rounded-control text-ink-soft group-hover:bg-nav-hover',
} as const;

const sizes = {
  rail: { org: 'size-11 text-[15px]', square: 'size-10 text-[15px]' },
  phone: { org: 'size-10 text-sm', square: 'size-[38px] text-sm' },
} as const;

const badgeAt = {
  rail: '-top-[7px] -right-[7px] h-5 min-w-5 px-[5px]',
  phone: '-top-1.5 -right-1.5 h-[18px] min-w-[18px] px-1',
} as const;

interface MarkProps {
  kind: keyof typeof kinds;
  size?: keyof typeof sizes;
  /** The current organization page or factory: a ring on the org mark, an ink fill on a factory. */
  current?: boolean;
  /** A violet count: what waits on you. Decorative; the trigger's name carries it. */
  badge?: number;
  /** A rust dot: blocked or andon pulled. Decorative; the trigger's name carries it. */
  dot?: boolean;
  children: ReactNode;
}

/**
 * The squares of the rail and the phone's top row (C §10.11). Purely visual (aria-hidden): the
 * link or button around it holds the accessible name, which says everything the mark shows.
 * Put it inside an element with `group` so hover reaches it.
 */
export function Mark({ kind, size = 'rail', current = false, badge = 0, dot = false, children }: MarkProps) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'relative box-border flex shrink-0 items-center justify-center tracking-[-0.01em] transition-[border-color,background-color,box-shadow] duration-200',
        kind === 'org' ? sizes[size].org : sizes[size].square,
        kinds[kind],
        current && kind === 'org' && 'shadow-[0_0_0_2px_var(--color-shell),0_0_0_4px_var(--color-ink)]',
        current && kind === 'factory' && 'border-ink bg-ink text-white group-hover:border-ink',
        current && kind === 'add' && 'border-solid border-ink bg-ink text-white',
        current && kind === 'icon' && 'bg-ink text-white group-hover:bg-ink',
      )}
    >
      {children}
      {badge > 0 ? (
        // The number is drawn by CSS, not text, so the mark's visible text stays its letters (the
        // trigger's name starts with them, WCAG 2.5.3) and the count is said once, in the name.
        <span
          data-count={badge}
          className={cn(
            'absolute box-border flex items-center justify-center rounded-chip border-2 border-shell bg-you text-[11px] font-semibold text-white after:content-[attr(data-count)]',
            badgeAt[size],
          )}
        />
      ) : null}
      {dot ? (
        <span className="absolute -right-1 -bottom-1 box-border size-3 rounded-full border-2 border-shell bg-rework" />
      ) : null}
    </span>
  );
}

/** The person's initials in a circle (the account, bottom of the rail). */
export function Avatar({ initials, size = 'rail' }: { initials: string; size?: 'rail' | 'phone' | 'card' }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex shrink-0 items-center justify-center rounded-full bg-muted font-semibold text-white',
        size === 'rail' && 'size-9 text-[13px]',
        size === 'phone' && 'size-[34px] text-xs',
        size === 'card' && 'size-10 text-sm',
      )}
    >
      {initials}
    </span>
  );
}
