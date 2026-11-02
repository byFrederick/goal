// A small radio group drawn as one concrete pill of options (C §10.11 `SegmentedControl`): Off /
// Ask me / Allowed, Write / Preview, a station's kind. Native radios in a fieldset: one tab stop,
// and the arrow keys move the choice, as in any radio group. A disabled option says why in a
// tooltip and in its accessible description; the arrow keys skip it. `readOnly` shows the choice
// without letting it change (a view-only editor): every option is disabled, the chosen one drawn
// as chosen.
// The chosen option sits on one white pill that slides to the new option (MOTION.md, Tabs and
// segmented controls). The pill is measured from the options, so it is placed before the first
// paint without a slide, and it slides only when the choice changes.
import { useId, useLayoutEffect, useRef } from 'react';
import { cn } from './cn';
import { placePill, tabPill } from './tab-nav';
import { Tooltip } from './tooltip';

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  /** Shown after the label ("Needs you 3"); the radio's accessible name includes it. */
  count?: number | undefined;
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
  /** `compact` is the top bar's size: 28 px segments in a 32 px control (1 px padding inside the border) from 768 px; phones keep 44 px segments. */
  size?: 'default' | 'compact';
  className?: string;
}

export function SegmentedControl<T extends string>({
  label,
  options,
  value,
  onChange,
  describedBy,
  readOnly = false,
  size = 'default',
  className,
}: SegmentedControlProps<T>) {
  const name = useId();
  const pill = useRef<HTMLSpanElement>(null);
  const labels = useRef<(HTMLLabelElement | null)[]>([]);
  const placed = useRef(false);

  // Moves the pill to the chosen option. The first placement, and a resize, snap; a new choice slides.
  useLayoutEffect(() => {
    const place = (animate: boolean) => {
      const el = pill.current;
      if (!el) return;
      const chosen = labels.current[options.findIndex((o) => o.value === value)];
      if (!chosen) {
        el.style.opacity = '0';
        return;
      }
      if (!animate) el.style.transition = 'none';
      el.style.opacity = '1';
      placePill(el, chosen);
      if (!animate) {
        void el.offsetWidth;
        el.style.transition = '';
      }
    };
    place(placed.current);
    placed.current = true;
    const onResize = () => place(false);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [options, value]);

  return (
    <fieldset
      aria-describedby={describedBy}
      className={cn(
        'relative m-0 inline-flex shrink-0 gap-px rounded-control border border-line bg-panel',
        size === 'compact' ? 'p-1 md:p-px' : 'p-1',
        className,
      )}
    >
      <legend className="sr-only">{label}</legend>
      <span ref={pill} aria-hidden="true" className={cn(tabPill, size === 'compact' && 'md:inset-y-px')} />
      {options.map((o, i) => {
        const checked = o.value === value;
        const reasonId = `${name}-${i}-reason`;
        const option = (
          <label
            key={o.value}
            ref={(el) => {
              labels.current[i] = el;
            }}
            className={cn(
              'relative inline-flex h-11 items-center gap-1 rounded-chip px-3 text-secondary whitespace-nowrap text-muted',
              size === 'compact' ? 'md:h-7' : 'md:h-7.5',
              readOnly ? 'cursor-default' : 'press cursor-pointer hover:text-ink',
              'has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-run',
              !readOnly &&
                'has-[:disabled]:cursor-not-allowed has-[:disabled]:text-muted has-[:disabled]:hover:text-muted',
              checked && 'font-control text-ink',
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
            {o.count !== undefined ? (
              <>
                {' '}
                <span className="tabular-nums text-muted">{o.count}</span>
              </>
            ) : null}
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
