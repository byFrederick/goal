import { Slot } from 'radix-ui';
import { type ComponentProps, type MouseEvent, type ReactNode, useEffect, useState } from 'react';
import { cn } from './cn';
import { Spinner } from './spinner';

// MASTER.md, Components: buttons are 32 px (28 px small) with the control radius and weight. Focus
// comes from the global :focus-visible rule (2 px cobalt, 2 px offset). Below md, sm and md grow to
// a 44 px minimum height (min-height beats h-7 and h-8). Icon buttons keep their drawn size and get
// a 44 px hit area from the pseudo-element on a coarse pointer.
const variants = {
  /** Near-black action fill with its ink text: the one primary action on a surface. Blue stays for running, links and focus. */
  primary: 'border-action bg-action text-action-ink hover:bg-action/90',
  /** Alias of primary, kept so callers do not change. */
  action: 'border-action bg-action text-action-ink hover:bg-action/90',
  /** White with a rule border: the secondary action. */
  secondary: 'border-rule bg-card text-ink hover:bg-panel',
  /** Alias of secondary, kept so callers do not change. */
  raised: 'border-rule bg-card text-ink hover:bg-panel',
  /** Alias of secondary, kept so callers do not change. */
  white: 'border-rule bg-card text-ink hover:bg-panel',
  /** Red text: destructive actions that are not the final confirmation. */
  danger: 'border-transparent bg-transparent text-rework-ink hover:bg-rework-tint',
  /** Red fill: the final confirmation of a destructive action. */
  'danger-fill': 'border-rework bg-rework text-white hover:bg-rework/90',
  /** Violet fill: Approve inside Needs you. */
  approve: 'border-you bg-you text-white hover:bg-you/90',
  /** Sends work back. Alias of danger, kept so callers do not change. */
  rework: 'border-transparent bg-transparent text-rework-ink hover:bg-rework-tint',
  /** Text only, for low-weight actions inside cards. */
  ghost: 'border-transparent bg-transparent text-ink hover:bg-hover',
} as const;

const sizes = {
  /** Inside cards: 28 px, at least 44 px below md. */
  sm: 'h-7 px-3 text-secondary max-md:min-h-11',
  /** The default: 32 px, at least 44 px below md. */
  md: 'h-8 px-3 text-body max-md:min-h-11',
  /** A large target for sign-in and dialogs: 44 px. */
  lg: 'h-11 px-4 text-body',
  /** An icon-only button: 32 px square. It must have an accessible name. */
  icon: 'size-8 justify-center px-0',
} as const;

type Variant = keyof typeof variants;

/** The Spinner's tone per variant: action-ink on the dark fills, ink on the light ones. */
const darkFills: ReadonlySet<Variant> = new Set(['primary', 'action', 'approve', 'danger-fill']);

/** MOTION.md, Loading: a quick answer never flashes the busy label. */
const busyDelayMs = 100;

type ButtonBase = ComponentProps<'button'> & {
  variant?: Variant;
  asChild?: boolean;
  /** A request is pending (MOTION.md, A button waits on the server). Pass `busy={mutation.isPending}`; keep `disabled` only for conditions that are not a pending request. */
  busy?: boolean;
  /** The work in progress, shown with a spinner after 100 ms of busy: "Publishing v41…". The button reserves the wider of this and its children. */
  busyLabel?: ReactNode;
};

export type ButtonProps =
  | (ButtonBase & { size?: Exclude<keyof typeof sizes, 'icon'> })
  | (ButtonBase & { size: 'icon'; 'aria-label': string });

// Busy composes with SuccessLabel by putting it in children:
//   <Button busy={mutation.isPending} busyLabel="Approving…">
//     <SuccessLabel done={done} doneLabel="Approved">Approve</SuccessLabel>
//   </Button>
// The idle label (with the done label once it shows) and the busy label share one grid cell, so the width is the
// widest of them. While busy the button sets aria-busy and aria-disabled, never `disabled`, so focus stays on it.
export function Button({
  variant = 'secondary',
  size = 'md',
  asChild,
  className,
  type,
  busy = false,
  busyLabel,
  children,
  onClick,
  ...props
}: ButtonProps) {
  const [showBusy, setShowBusy] = useState(false);
  useEffect(() => {
    if (!busy) {
      setShowBusy(false);
      return;
    }
    const timer = setTimeout(() => setShowBusy(true), busyDelayMs);
    return () => clearTimeout(timer);
  }, [busy]);

  const busyOn = busy && !asChild;
  const classes = cn(
    'press relative inline-flex shrink-0 items-center gap-2 rounded-control border font-control no-underline whitespace-nowrap',
    'pointer-coarse:before:absolute pointer-coarse:before:-inset-2 pointer-coarse:before:content-[""]',
    'disabled:cursor-not-allowed disabled:opacity-55 [&_svg]:shrink-0',
    busyOn ? 'cursor-progress' : 'aria-disabled:cursor-not-allowed aria-disabled:opacity-55',
    variants[variant],
    sizes[size],
    className,
  );

  if (asChild) {
    return (
      <Slot.Root className={classes} data-variant={variant} onClick={onClick} {...props}>
        {children}
      </Slot.Root>
    );
  }

  const handleClick = (event: MouseEvent<HTMLButtonElement>) => {
    if (busy) {
      event.preventDefault();
      return;
    }
    onClick?.(event);
  };

  const cell = 'col-start-1 row-start-1 inline-flex items-center justify-center gap-2';
  const content =
    busyLabel === undefined ? (
      children
    ) : (
      <span className="inline-grid">
        <span className={cn(cell, showBusy && 'invisible')} aria-hidden={showBusy || undefined} data-slot="idle">
          {children}
        </span>
        <span
          className={cn(cell, !showBusy && 'invisible', showBusy && 'motion-safe:animate-busy-swap')}
          aria-hidden={!showBusy}
          data-slot="busy"
        >
          <Spinner tone={darkFills.has(variant) ? 'light' : 'dark'} className="motion-reduce:animate-none" />
          {busyLabel}
        </span>
      </span>
    );

  return (
    <button
      type={type ?? 'button'}
      className={classes}
      data-variant={variant}
      {...props}
      aria-busy={busy ? 'true' : props['aria-busy']}
      aria-disabled={busy ? 'true' : props['aria-disabled']}
      onClick={handleClick}
    >
      {content}
    </button>
  );
}
