// A list of short values as a token field (D §10.5): a chip per value with "Remove <noun> x",
// and an input where Enter or a comma adds the typed value once it passes `check` and is not
// listed yet. Basics' work types and tools. View only, the chips alone.
import { X } from 'lucide-react';
import { type KeyboardEvent, type ReactNode, useId, useState } from 'react';
import { cn } from '../../../ui/cn';
import { useViewOnly } from '../draft/use-draft';

export function TokenField({
  label,
  aside,
  noun,
  values,
  placeholder,
  check,
  problem,
  help,
  mono = false,
  onAdd,
  onRemove,
}: {
  label: string;
  /** Beside the label: "Edited · Undo". */
  aside?: ReactNode;
  /** What one value is, for "Add a work type" and "Remove work type feature". */
  noun: string;
  values: readonly string[];
  placeholder: string;
  /** Why a typed value can't be added; undefined when it can. */
  check: (value: string) => string | undefined;
  /** B's problem with the list. */
  problem?: string | undefined;
  help: ReactNode;
  /** Values in JetBrains Mono (tool specs). */
  mono?: boolean;
  onAdd: (value: string) => void;
  onRemove: (value: string) => void;
}) {
  const viewOnly = useViewOnly();
  const id = useId();
  const [typed, setTyped] = useState('');
  const [error, setError] = useState<string | null>(null);
  const shownError = error ?? problem;

  const add = () => {
    const value = typed.trim();
    if (value === '') return;
    const refusal = values.includes(value) ? `${value} is already a ${noun}.` : check(value);
    if (refusal) {
      setError(refusal);
      return;
    }
    onAdd(value);
    setTyped('');
    setError(null);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      add();
    }
  };

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div className="flex min-h-6 flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <span id={`${id}-label`} className="text-sm font-medium">
          {label}
        </span>
        {aside}
      </div>
      <fieldset
        aria-labelledby={`${id}-label`}
        className={cn(
          'm-0 flex min-h-11 flex-wrap items-center gap-1.5 rounded-control border bg-card px-1.5 py-1 transition-colors duration-200 md:min-h-10',
          // The input inside draws no ring of its own: the whole field shows focus.
          'has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-2 has-[input:focus-visible]:outline-run',
          viewOnly ? 'border-line bg-shell' : shownError ? 'border-rework' : 'border-rule hover:border-muted',
        )}
      >
        {viewOnly && values.length === 0 ? <span className="px-1.5 text-sm text-muted">None</span> : null}
        {values.map((value) => (
          <span
            key={value}
            className={cn(
              'inline-flex h-8 items-center gap-1 rounded-chip border border-line bg-card pr-1 pl-2.5 text-[13px]',
              mono && 'font-mono',
            )}
          >
            {value}
            {viewOnly ? (
              <span className="w-1" />
            ) : (
              <button
                type="button"
                aria-label={`Remove ${noun} ${value}`}
                onClick={() => onRemove(value)}
                className="inline-flex size-6 items-center justify-center rounded-chip text-muted transition-colors duration-200 hover:bg-shell hover:text-ink"
              >
                <X size={14} strokeWidth={1.5} aria-hidden="true" />
              </button>
            )}
          </span>
        ))}
        {viewOnly ? null : (
          <input
            value={typed}
            onChange={(e) => {
              setTyped(e.target.value);
              setError(null);
            }}
            onKeyDown={onKeyDown}
            onBlur={add}
            aria-label={`Add a ${noun}`}
            aria-describedby={`${id}-help`}
            aria-invalid={shownError ? true : undefined}
            placeholder={placeholder}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            className={cn(
              'h-8 min-w-[110px] flex-1 border-0 bg-transparent px-1.5 text-base text-ink placeholder:text-muted focus-visible:outline-none md:text-sm',
              mono && 'font-mono',
            )}
          />
        )}
      </fieldset>
      <div id={`${id}-help`} className="flex flex-col gap-0.5 text-[13px] leading-snug">
        {shownError ? (
          <p role="alert" className="m-0 text-rework-ink">
            {shownError}
          </p>
        ) : null}
        <div className="m-0 text-muted">{help}</div>
      </div>
    </div>
  );
}
