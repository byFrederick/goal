// An on/off control (C §10.11 `Switch`): a button with role="switch" whose name and description
// come from the text beside it. 32 × 18 as drawn, `rule` when off and `ink` when on, with a 44 px hit area on phones. A disabled
// switch stays focusable (aria-disabled), so the reason it describes can be heard. The thumb travels
// with a slight overshoot and the track colour fades (MOTION.md, Switch).
import type { ComponentProps } from 'react';
import { cn } from './cn';
import { useReadOnly } from './read-only';

type SwitchProps = Omit<ComponentProps<'button'>, 'onChange' | 'role'> & {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  /** Shown as aria-disabled: focusable, does nothing. */
  disabled?: boolean;
};

export function Switch({ checked, onCheckedChange, disabled: off, className, ...props }: SwitchProps) {
  const shown = useReadOnly();
  const disabled = off || shown;
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-disabled={disabled || undefined}
      onClick={() => {
        if (!disabled) onCheckedChange(!checked);
      }}
      className={cn(
        'relative h-4.5 w-8 shrink-0 rounded-chip bg-rule transition-colors duration-(--duration-quick)',
        'before:absolute before:-inset-x-1.5 before:-inset-y-3.25 before:content-[""] md:before:-inset-1',
        'aria-checked:bg-ink',
        'aria-disabled:cursor-not-allowed aria-disabled:opacity-60',
        className,
      )}
      {...props}
    >
      <span
        aria-hidden="true"
        className={cn(
          'absolute top-0.5 left-0.5 size-3.5 rounded-full bg-card transition-transform duration-(--duration-medium) ease-bounce',
          checked && 'translate-x-3.5',
        )}
      />
    </button>
  );
}
