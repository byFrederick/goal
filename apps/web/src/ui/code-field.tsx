// The sign-in code (spec 3 §10.1, accessibility): one input, shown as six cells "123 456". It is
// a real text field laid over the cells and transparent, so paste, the phone's code suggestion
// (autocomplete one-time-code) and assistive technology all meet one field with its label.
import { type ComponentProps, Fragment } from 'react';
import { cn } from './cn';
import { useFieldControl } from './field';

export const codeLength = 6;

/** The digits of what was typed or pasted ("482 913", "482-913"), at most six. */
export const codeDigits = (text: string) => text.replace(/\D/g, '').slice(0, codeLength);

interface CodeFieldProps extends Omit<ComponentProps<'input'>, 'value' | 'onChange'> {
  value: string;
  onChange: (digits: string) => void;
  /** error: the code was refused (rust cells); off: it is void, nothing to type until a new one. */
  look?: 'normal' | 'error' | 'off';
}

const positions = Array.from({ length: codeLength }, (_, i) => i);

export function CodeField({ value, onChange, look = 'normal', className, disabled, ...props }: CodeFieldProps) {
  const field = useFieldControl();
  const off = look === 'off';
  // The cell that takes the next digit: an ink border and a caret, and the focus ring while focused.
  const next = value.length < codeLength && !off ? value.length : -1;
  return (
    <div className={cn('group relative', className)}>
      <input
        id={field?.id}
        aria-describedby={field?.describedBy}
        aria-invalid={field?.invalid || look === 'error' || undefined}
        inputMode="numeric"
        autoComplete="one-time-code"
        pattern="[0-9]*"
        // "123 456" pasted with its space still fits; only the digits are kept.
        maxLength={codeLength + 1}
        spellCheck={false}
        disabled={disabled || off}
        value={value}
        onChange={(e) => onChange(codeDigits(e.target.value))}
        className="absolute inset-0 z-10 size-full cursor-text border-0 bg-transparent p-0 text-base opacity-0 outline-none disabled:cursor-not-allowed"
        {...props}
      />
      <div aria-hidden="true" className="grid grid-cols-[repeat(3,minmax(0,1fr))_10px_repeat(3,minmax(0,1fr))] gap-2">
        {positions.map((i) => (
          <Fragment key={i}>
            {i === 3 ? <span /> : null}
            <span
              className={cn(
                'flex h-14 min-w-0 items-center justify-center rounded-control border bg-surface text-2xl text-ink transition-colors duration-200',
                look === 'normal' && 'border-rule',
                look === 'error' && 'border-rework',
                off && 'border-line bg-shell text-muted',
                i === next && 'border-ink shadow-[0_0_0_1px_var(--color-ink)]',
                i === next &&
                  'group-focus-within:outline-2 group-focus-within:outline-offset-2 group-focus-within:outline-run group-focus-within:outline-solid',
              )}
            >
              {value[i] ?? (i === next ? <span className="h-[26px] w-0.5 bg-ink" /> : null)}
            </span>
          </Fragment>
        ))}
      </div>
    </div>
  );
}
