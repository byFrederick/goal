import { Slot } from 'radix-ui';
import type { ComponentProps } from 'react';
import { cn } from './cn';

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

type ButtonBase = ComponentProps<'button'> & {
  variant?: Variant;
  asChild?: boolean;
};

export type ButtonProps =
  | (ButtonBase & { size?: Exclude<keyof typeof sizes, 'icon'> })
  | (ButtonBase & { size: 'icon'; 'aria-label': string });

export function Button({ variant = 'secondary', size = 'md', asChild, className, type, ...props }: ButtonProps) {
  const classes = cn(
    'press relative inline-flex shrink-0 items-center gap-2 rounded-control border font-control no-underline whitespace-nowrap',
    'pointer-coarse:before:absolute pointer-coarse:before:-inset-2 pointer-coarse:before:content-[""]',
    'disabled:cursor-not-allowed disabled:opacity-55 aria-disabled:cursor-not-allowed aria-disabled:opacity-55 [&_svg]:shrink-0',
    variants[variant],
    sizes[size],
    className,
  );

  if (asChild) {
    return <Slot.Root className={classes} data-variant={variant} {...props} />;
  }

  return <button type={type ?? 'button'} className={classes} data-variant={variant} {...props} />;
}
