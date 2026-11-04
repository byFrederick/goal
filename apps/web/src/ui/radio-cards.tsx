// Radio groups drawn as cards (C §10.11 `RadioCards`: the wizard's four ways to start) and as
// rows of a list (the templates, the factories to clone). Native radios in a fieldset: one tab
// stop, the arrow keys move the choice, the label is the whole card. The chosen one has an ink
// border, drawn inside so nothing moves.
import type { ReactNode } from 'react';
import { cn } from './cn';
import { sectionHeadingClass } from './page-header';

export interface RadioOption<T extends string> {
  value: T;
  label: ReactNode;
  description?: ReactNode;
  /** A card's icon (decorative). */
  icon?: ReactNode;
  /** A row's far end: "6 stations, 3 agents". */
  aside?: ReactNode;
  /** A card that cannot be chosen yet; it shows dimmed and the radio is disabled. */
  disabled?: boolean;
  /** Beside a card's title ("Later"). */
  badge?: ReactNode;
}

interface RadioGroupProps<T extends string> {
  /** The group's name, said once ("How do you want to start?"); `legend` shows it. */
  legend: ReactNode;
  /** Show the legend (else it is for screen readers only). */
  showLegend?: boolean;
  /** The radios' name attribute. */
  name: string;
  options: readonly RadioOption<T>[];
  value: T | undefined;
  onChange: (value: T) => void;
  className?: string;
  /** The cards' grid columns, when the default (2, then 4) does not fit the number of options. */
  gridClassName?: string;
  /** The id of the text that describes the whole group. */
  describedBy?: string;
}

const radio =
  'm-0 size-5 shrink-0 cursor-pointer appearance-none rounded-full border border-rule bg-card transition-[border-width,border-color] duration-(--duration-quick) checked:border-[6px] checked:border-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run';

const chosen = 'border-ink shadow-[inset_0_0_0_1px_var(--color-ink)]';

/** Cards side by side: icon, title, description, the radio top right. */
export function RadioCards<T extends string>({
  legend,
  showLegend = true,
  name,
  options,
  value,
  onChange,
  className,
  gridClassName,
}: RadioGroupProps<T>) {
  return (
    <fieldset className={cn('m-0 min-w-0 border-0 p-0', className)}>
      <legend className={showLegend ? cn(sectionHeadingClass, 'mb-3 p-0') : 'sr-only'}>{legend}</legend>
      <div className={cn('grid gap-3', gridClassName ?? '@[560px]:grid-cols-2 @[1100px]:grid-cols-4')}>
        {options.map((o) => (
          <label
            key={o.value}
            className={cn(
              'relative flex min-w-0 cursor-pointer flex-col gap-2 rounded-card border border-line bg-card p-4 transition-colors duration-(--duration-quick) hover:border-rule',
              o.value === value && chosen,
              o.disabled && 'cursor-not-allowed opacity-55 hover:border-line',
            )}
          >
            <span className="flex items-start justify-between gap-3">
              {o.icon ? (
                <span
                  aria-hidden="true"
                  className="flex size-10 items-center justify-center rounded-control bg-panel text-ink-soft"
                >
                  {o.icon}
                </span>
              ) : null}
              <input
                type="radio"
                name={name}
                value={o.value}
                checked={o.value === value}
                disabled={o.disabled ?? false}
                onChange={() => onChange(o.value)}
                className={radio}
              />
            </span>
            <span className="mt-1 flex items-center gap-2 text-[17px] font-medium tracking-[-0.01em]">
              {o.label}
              {o.badge}
            </span>
            {o.description ? <span className="text-sm leading-normal text-muted">{o.description}</span> : null}
          </label>
        ))}
      </div>
    </fieldset>
  );
}

/** Rows of a list: the radio, a title with a line under it, an aside at the far end. */
export function RadioList<T extends string>({
  legend,
  showLegend = false,
  name,
  options,
  value,
  onChange,
  className,
  describedBy,
}: RadioGroupProps<T>) {
  return (
    <fieldset aria-describedby={describedBy} className={cn('m-0 min-w-0 border-0 p-0', className)}>
      <legend className={showLegend ? 'mb-2 p-0 text-sm font-medium' : 'sr-only'}>{legend}</legend>
      <div className="@container flex flex-col gap-2">
        {options.map((o) => (
          <label
            key={o.value}
            className={cn(
              'flex min-h-14 min-w-0 cursor-pointer items-center gap-3.5 rounded-control border border-line bg-card px-4 py-2.5 transition-colors duration-(--duration-quick) hover:border-rule',
              o.value === value && chosen,
            )}
          >
            <input
              type="radio"
              name={name}
              value={o.value}
              checked={o.value === value}
              onChange={() => onChange(o.value)}
              className={radio}
            />
            <span className="flex min-w-0 flex-1 flex-col gap-x-4 gap-y-0.5 @[480px]:flex-row @[480px]:items-center">
              <span className="min-w-0 flex-1">
                <span className="block text-[15px] leading-snug font-medium break-words">{o.label}</span>
                {o.description ? (
                  <span className="block text-[13px] leading-snug text-muted">{o.description}</span>
                ) : null}
              </span>
              {o.aside ? <span className="shrink-0 text-[13px] text-muted">{o.aside}</span> : null}
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}
