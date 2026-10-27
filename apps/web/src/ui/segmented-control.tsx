// A small radio group drawn as one concrete pill of options (C §10.11 `SegmentedControl`): Off /
// Ask me / Allowed, Write / Preview, a station's kind. Native radios in a fieldset: one tab stop,
// and the arrow keys move the choice, as in any radio group. A disabled option says why in a
// tooltip and in its accessible description; the arrow keys skip it. `readOnly` shows the choice
// without letting it change (a view-only editor): every option is disabled, the chosen one drawn
// as chosen.
import { useId } from 'react';
import { cn } from './cn';
import { Tooltip } from './tooltip';

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  /** Why the option cannot be chosen; the option is disabled when set. */
  disabledReason?: string | undefined;
}

interface SegmentedControlProps<T extends string> {
  /** The group's name ("Read files", "Prompt view", "Kind of Build"). */
  label: string;
  options: readonly SegmentedOption<T>[];
  value: T | undefined;
  onChange: (value: T) => void;
  /** Ids of what describes the group (a warning under it). */
  describedBy?: string | undefined;
  /** Shows the choice only. */
  readOnly?: boolean;
  className?: string;
}

export function SegmentedControl<T extends string>({
  label,
  options,
  value,
  onChange,
  describedBy,
  readOnly = false,
  className,
}: SegmentedControlProps<T>) {
  const name = useId();
  return (
    <fieldset
      aria-describedby={describedBy}
      className={cn('m-0 inline-flex shrink-0 gap-0.5 rounded-control border-0 bg-panel p-[3px]', className)}
    >
      <legend className="sr-only">{label}</legend>
      {options.map((o, i) => {
        const checked = o.value === value;
        const reasonId = `${name}-${i}-reason`;
        const option = (
          <label
            key={o.value}
            className={cn(
              'relative inline-flex h-11 cursor-pointer items-center rounded-chip px-2.5 text-[13px] whitespace-nowrap text-muted transition-colors duration-200 hover:text-ink md:h-[30px]',
              'has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-run',
              readOnly
                ? 'cursor-default hover:text-muted'
                : 'has-[:disabled]:cursor-not-allowed has-[:disabled]:text-muted has-[:disabled]:hover:text-muted',
              checked && 'bg-card font-medium text-ink hover:text-ink',
            )}
          >
            <input
              type="radio"
              name={name}
              value={o.value}
              checked={checked}
              disabled={readOnly || Boolean(o.disabledReason)}
              aria-describedby={o.disabledReason && !readOnly ? reasonId : undefined}
              onChange={() => onChange(o.value)}
              className={cn(
                'absolute inset-0 m-0 cursor-pointer appearance-none opacity-0',
                readOnly ? 'disabled:cursor-default' : 'disabled:cursor-not-allowed',
              )}
            />
            {o.label}
            {o.disabledReason && !readOnly ? (
              <span id={reasonId} className="sr-only">
                {o.disabledReason}
              </span>
            ) : null}
          </label>
        );
        return o.disabledReason && !readOnly ? (
          <Tooltip key={o.value} label={o.disabledReason} side="top">
            {option}
          </Tooltip>
        ) : (
          option
        );
      })}
    </fieldset>
  );
}
