// A small radio group drawn as one concrete pill of options (C §10.11 `SegmentedControl`): Off /
// Ask me / Allowed, Write / Preview. Native radios in a fieldset: one tab stop, and the arrow
// keys move the choice, as in any radio group.
import { useId } from 'react';
import { cn } from './cn';

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
}

interface SegmentedControlProps<T extends string> {
  /** The group's name ("Read files", "Prompt view"). */
  label: string;
  options: readonly SegmentedOption<T>[];
  value: T | undefined;
  onChange: (value: T) => void;
  /** Ids of what describes the group (a warning under it). */
  describedBy?: string | undefined;
  className?: string;
}

export function SegmentedControl<T extends string>({
  label,
  options,
  value,
  onChange,
  describedBy,
  className,
}: SegmentedControlProps<T>) {
  const name = useId();
  return (
    <fieldset
      aria-describedby={describedBy}
      className={cn('m-0 inline-flex shrink-0 gap-0.5 rounded-control border-0 bg-control p-[3px]', className)}
    >
      <legend className="sr-only">{label}</legend>
      {options.map((o) => {
        const checked = o.value === value;
        return (
          <label
            key={o.value}
            className={cn(
              'relative inline-flex h-11 cursor-pointer items-center rounded-chip px-2.5 text-[13px] whitespace-nowrap text-muted transition-colors duration-200 hover:text-ink md:h-[30px]',
              'has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-run',
              checked && 'bg-surface font-medium text-ink shadow-raise',
            )}
          >
            <input
              type="radio"
              name={name}
              value={o.value}
              checked={checked}
              onChange={() => onChange(o.value)}
              className="absolute inset-0 m-0 cursor-pointer appearance-none opacity-0"
            />
            {o.label}
          </label>
        );
      })}
    </fieldset>
  );
}
