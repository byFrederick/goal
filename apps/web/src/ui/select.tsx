// A select on Radix Select: a 32 px trigger button that opens a listbox (MOTION.md, "Menu, select,
// popover, combobox"). Arrow keys, Home/End and typeahead move between options, Enter chooses,
// Escape closes and returns focus to the trigger. It replaces the browser's native select.
import { Check, ChevronDown } from 'lucide-react';
import { Select as S } from 'radix-ui';
import { cn } from './cn';
import { useFieldControl } from './field';
import { useReadOnly } from './read-only';

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

/** Radix forbids an empty item value; the option whose value is '' travels under this one. */
const EMPTY = '__empty__';
const toRadix = (value: string) => (value === '' ? EMPTY : value);
const fromRadix = (value: string) => (value === EMPTY ? '' : value);

interface SelectProps {
  options: readonly SelectOption[];
  value?: string | undefined;
  defaultValue?: string | undefined;
  onValueChange?: (value: string) => void | undefined;
  /** Shown while no option is chosen and none has the value ''. */
  placeholder?: string | undefined;
  id?: string | undefined;
  name?: string | undefined;
  'aria-label'?: string | undefined;
  'aria-describedby'?: string | undefined;
  'aria-invalid'?: boolean | undefined;
  'aria-busy'?: boolean | undefined;
  disabled?: boolean | undefined;
  defaultOpen?: boolean | undefined;
  onOpenChange?: (open: boolean) => void | undefined;
  className?: string | undefined;
  /** Classes for the open list, for instance a wider panel than the trigger. */
  contentClassName?: string | undefined;
}

export function Select({
  options,
  value,
  defaultValue,
  onValueChange,
  placeholder,
  id,
  name,
  disabled,
  defaultOpen,
  onOpenChange,
  className,
  contentClassName,
  ...aria
}: SelectProps) {
  const field = useFieldControl();
  const shown = useReadOnly();
  const hasEmpty = options.some((o) => o.value === '');
  const radixValue = value === undefined ? undefined : hasEmpty || value !== '' ? toRadix(value) : '';
  return (
    <S.Root
      {...(radixValue === undefined ? {} : { value: radixValue })}
      {...(defaultValue === undefined ? {} : { defaultValue: toRadix(defaultValue) })}
      {...(name === undefined ? {} : { name })}
      {...(defaultOpen === undefined ? {} : { defaultOpen })}
      {...(onOpenChange === undefined ? {} : { onOpenChange })}
      onValueChange={(next) => onValueChange?.(fromRadix(next))}
      disabled={disabled || shown}
    >
      <S.Trigger
        id={id ?? field?.id}
        aria-describedby={aria['aria-describedby'] ?? field?.describedBy}
        aria-invalid={aria['aria-invalid'] ?? (field?.invalid || undefined)}
        aria-busy={aria['aria-busy']}
        aria-label={aria['aria-label']}
        className={cn(
          'flex h-11 w-full min-w-0 cursor-pointer items-center justify-between gap-2 rounded-control border border-rule bg-card px-3 text-left text-base text-ink transition-colors duration-(--duration-quick) md:h-8 md:text-body',
          'hover:border-muted aria-[invalid=true]:border-rework data-[placeholder]:text-muted',
          'disabled:cursor-default disabled:border-line disabled:bg-shell disabled:hover:border-line',
          className,
        )}
      >
        <span className="min-w-0 truncate">
          <S.Value {...(placeholder === undefined ? {} : { placeholder })} />
        </span>
        <S.Icon asChild>
          <ChevronDown size={16} strokeWidth={1.75} aria-hidden="true" className="shrink-0 text-muted" />
        </S.Icon>
      </S.Trigger>
      <S.Portal>
        <S.Content
          position="popper"
          sideOffset={4}
          collisionPadding={8}
          className={cn(
            'motion-panel motion-menu z-50 max-h-(--radix-select-content-available-height) min-w-(--radix-select-trigger-width) max-w-(--radix-select-content-available-width) overflow-hidden rounded-card bg-card p-1 text-ink shadow-pop outline-none',
            contentClassName,
          )}
        >
          <S.Viewport>
            {options.map((o) => (
              <S.Item
                key={o.value}
                value={toRadix(o.value)}
                {...(o.disabled === undefined ? {} : { disabled: o.disabled })}
                className="flex min-h-11 w-full cursor-pointer items-center justify-between gap-3 rounded-control px-3 text-left text-body text-ink outline-none transition-colors duration-quick focus-visible:-outline-offset-2 data-[disabled]:cursor-default data-[disabled]:text-muted data-[highlighted]:bg-shell md:min-h-7.5"
              >
                <S.ItemText>{o.label}</S.ItemText>
                <S.ItemIndicator className="flex">
                  <Check size={18} strokeWidth={1.75} aria-hidden="true" />
                </S.ItemIndicator>
              </S.Item>
            ))}
          </S.Viewport>
        </S.Content>
      </S.Portal>
    </S.Root>
  );
}
