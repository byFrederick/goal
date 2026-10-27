// An on/off control (C §10.11 `Switch`): a button with role="switch" whose name and description
// come from the text beside it. 40 × 24 as drawn, with a 44 px hit area on phones. A disabled
// switch stays focusable (aria-disabled), so the reason it describes can be heard.
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
        'relative h-6 w-10 shrink-0 rounded-chip border border-rule bg-panel transition-colors duration-200',
        'before:absolute before:-inset-2.5 before:content-[""] md:before:-inset-1',
        'aria-checked:border-run aria-checked:bg-run',
        'aria-disabled:cursor-not-allowed aria-disabled:border-rule aria-disabled:opacity-60',
        className,
      )}
      {...props}
    >
      <span
        aria-hidden="true"
        className={cn(
          'absolute top-0.75 left-0.75 size-4 rounded-full bg-card transition-transform duration-200',
          checked && 'translate-x-4',
        )}
      />
    </button>
  );
}
