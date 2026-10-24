import { Slot } from 'radix-ui';
import type { ComponentProps } from 'react';
import { cn } from './cn';

const variants = {
  /** Ink: the one primary action on a surface. */
  primary: 'bg-ink text-white hover:bg-ink/90',
  /** Concrete: secondary actions. */
  secondary: 'bg-control text-ink hover:bg-control-hover',
  /** White on concrete, for toggles and status pills. */
  raised: 'bg-surface text-ink shadow-raise hover:bg-shell',
  /** White with a hairline: a secondary action beside a primary one in a page header. */
  white: 'border border-line bg-surface text-ink hover:bg-card',
  /** Sends work back: dark rust text (AA on concrete), never a red fill. */
  rework: 'bg-control text-rework-ink hover:bg-control-hover',
  /** Text only, for low-weight actions inside cards. */
  ghost: 'bg-transparent text-ink hover:bg-nav-hover',
} as const;

const sizes = {
  /** Inside cards (32 px; a pointer target, so desktop cards only). */
  sm: 'h-8 px-2.5 text-[13px]',
  md: 'h-10 px-3.5 text-sm',
  lg: 'h-11 px-4 text-[15px]',
  icon: 'size-10 justify-center',
} as const;

export type ButtonProps = ComponentProps<'button'> & {
  variant?: keyof typeof variants;
  size?: keyof typeof sizes;
  asChild?: boolean;
};

export function Button({ variant = 'secondary', size = 'md', asChild, className, type, ...props }: ButtonProps) {
  const classes = cn(
    'inline-flex shrink-0 items-center gap-2 rounded-control font-medium no-underline transition-colors duration-200',
    'disabled:cursor-not-allowed disabled:opacity-55 aria-disabled:cursor-not-allowed aria-disabled:opacity-55 [&_svg]:shrink-0',
    variants[variant],
    sizes[size],
    className,
  );

  if (asChild) {
    return <Slot.Root className={classes} {...props} />;
  }

  return <button type={type ?? 'button'} className={classes} {...props} />;
}
