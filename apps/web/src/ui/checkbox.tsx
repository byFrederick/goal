// A checkbox (MOTION.md, Checkbox): a native input, visually hidden, so the keyboard and the screen
// reader see the real control, and a box drawn beside it. The box fills first, then the check draws
// (styles.css, .check-box). The name is the visible label, or aria-label when there is no label.
import type { ComponentProps, ReactNode } from 'react';
import { cn } from './cn';

interface CheckboxProps extends Omit<ComponentProps<'label'>, 'onChange' | 'children'> {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  /** Shown as a disabled box: it does not change when clicked or when Space is pressed. */
  disabled?: boolean;
  /** The visible name, beside the box. Give aria-label instead when there is none. */
  label?: ReactNode;
  'aria-label'?: string;
  name?: string;
  /** A box whose feature is not built: it stays focusable with aria-disabled, shows its saved value and never changes (ADR 0016, decision 4). */
  unavailable?: boolean;
}

export function Checkbox({
  checked,
  onCheckedChange,
  disabled = false,
  label,
  name,
  unavailable = false,
  className,
  'aria-label': ariaLabel,
  ...props
}: CheckboxProps) {
  return (
    <label
      className={cn(
        'inline-flex items-center gap-2 text-body text-ink',
        disabled || unavailable ? 'cursor-not-allowed opacity-55' : 'cursor-pointer',
        className,
      )}
      {...props}
    >
      <input
        type="checkbox"
        name={name}
        checked={checked}
        disabled={disabled}
        aria-label={ariaLabel}
        aria-disabled={unavailable || undefined}
        onChange={(event) => {
          if (!disabled && !unavailable) onCheckedChange(event.currentTarget.checked);
        }}
        className="peer sr-only"
      />
      <span
        aria-hidden="true"
        data-checked={checked || undefined}
        className="check-box relative flex size-4 shrink-0 items-center justify-center rounded-chip border border-rule bg-card transition-[background-color,border-color] duration-(--duration-quick) peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-run data-checked:border-run data-checked:bg-run"
      >
        <svg
          viewBox="0 0 12 12"
          aria-hidden="true"
          className="size-3"
          fill="none"
          stroke="currentColor"
          strokeWidth={1.75}
        >
          <path
            d="M2.5 6.2 5 8.6 9.5 3.6"
            pathLength={1}
            strokeLinecap="round"
            strokeLinejoin="round"
            className="text-white"
          />
        </svg>
      </span>
      {label}
    </label>
  );
}
