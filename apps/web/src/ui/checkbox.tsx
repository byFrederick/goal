// A checkbox (MOTION.md, Checkbox): a native input, visually hidden, so the keyboard and the screen
// reader see the real control, and a box drawn beside it. The box fills first, then the check draws
// (styles.css, .check-box). The name is the visible label, or aria-label when there is no label.
import type { ComponentProps, ReactNode } from 'react';
import { cn } from './cn';

/** What a wrapper (NotAvailable, a tooltip trigger) may put on the native input: handlers, aria, a ref. */
type InputPassThrough = Omit<
  ComponentProps<'input'>,
  'type' | 'checked' | 'disabled' | 'onChange' | 'className' | 'name'
>;

interface CheckboxProps extends InputPassThrough {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  /** Shown as a disabled box: it does not change when clicked or when Space is pressed. */
  disabled?: boolean;
  /** The visible name, beside the box. Give aria-label instead when there is none. */
  label?: ReactNode;
  name?: string;
  className?: string;
}

export function Checkbox({
  checked,
  onCheckedChange,
  disabled = false,
  label,
  name,
  className,
  ...input
}: CheckboxProps) {
  return (
    <label
      className={cn(
        'inline-flex items-center gap-2 text-body text-ink',
        disabled ? 'cursor-not-allowed opacity-55' : 'cursor-pointer',
        className,
      )}
    >
      <input
        type="checkbox"
        name={name}
        checked={checked}
        disabled={disabled}
        {...input}
        onClick={(event) => {
          input.onClick?.(event);
          // A wrapper that cancels the click (NotAvailable) makes the browser revert the toggle after React
          // restored the controlled value, which leaves the box flipped. Put the saved value back once the
          // cancelled click is over.
          if (event.defaultPrevented) {
            const el = event.currentTarget;
            setTimeout(() => {
              el.checked = checked;
            }, 0);
          }
        }}
        onChange={(event) => {
          if (!disabled) onCheckedChange(event.currentTarget.checked);
        }}
        className="peer sr-only"
      />
      <span
        aria-hidden="true"
        data-checked={checked || undefined}
        className="check-box pointer-events-none relative flex size-4 shrink-0 items-center justify-center rounded-chip border border-rule bg-card transition-[background-color,border-color] duration-(--duration-quick) peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-run data-checked:border-run data-checked:bg-run"
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
