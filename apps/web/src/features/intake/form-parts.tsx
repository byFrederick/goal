// The pieces the trigger's blocks share (MASTER.md, "Sentence builder"): the block card, its problems, a
// count, a check box with its words, and a list of values as removable chips. The trigger form, the When
// block and the check section all draw from here, so a control looks the same in each.
import { X } from 'lucide-react';
import { type KeyboardEvent, type ReactNode, useId, useState } from 'react';
import { cn } from '../../ui/cn';
import { Input } from '../../ui/field';
import { InfoTip } from '../jonah/info-tip';

/** The problems of each block, by the block the problem's line sits in. */
export type BlockProblems = Partial<Record<'on' | 'filter' | 'only' | 'check' | 'create' | 'limits', string[]>>;

/** A block of the form: the picture's card with its header bar, its ⓘ and its problems under the controls. */
export function Block({
  id,
  title,
  info,
  hint,
  problems = [],
  children,
}: {
  id: string;
  title: string;
  /** What the ⓘ beside the title says; the block has no explanatory paragraph. */
  info?: string | undefined;
  hint?: string | undefined;
  problems?: string[] | undefined;
  children: ReactNode;
}) {
  return (
    <section aria-labelledby={id} className="overflow-hidden rounded-card border border-line">
      <header className="flex flex-wrap items-center gap-2 border-b border-line bg-panel px-4 py-3">
        <h2 id={id} className="m-0 text-body font-semibold">
          {title}
        </h2>
        {info ? <InfoTip label={`About ${title.toLowerCase()}`}>{info}</InfoTip> : null}
        {hint ? <p className="m-0 ml-auto text-secondary text-muted">{hint}</p> : null}
      </header>
      <div className="flex flex-col gap-3 p-4">
        {children}
        <Problems messages={problems} />
      </div>
    </section>
  );
}

/** Validation problems under the field they belong to: the server's words, never a guess of ours. */
export function Problems({ messages }: { messages: string[] }) {
  return messages.map((message) => (
    <p key={message} role="alert" className="motion-safe:animate-error-shake m-0 text-secondary text-rework-ink">
      {message}
    </p>
  ));
}

/** A whole number of at least one, typed in place. Text that is not one is not saved; it resets on blur. */
export function Count({
  label,
  value,
  onChange,
  disabled,
}: {
  label: string;
  value: number;
  onChange: (n: number) => void;
  disabled?: boolean;
}) {
  const [text, setText] = useState(String(value));
  return (
    <Input
      type="number"
      inputMode="numeric"
      min={1}
      step={1}
      aria-label={label}
      disabled={disabled}
      value={text}
      onChange={(e) => {
        setText(e.target.value);
        const n = Number(e.target.value);
        if (Number.isInteger(n) && n >= 1) onChange(n);
      }}
      onBlur={() => setText(String(value))}
      className="h-8 w-16 text-center"
    />
  );
}

/** A check box with its words beside it. */
export function Option({
  checked,
  onChange,
  children,
}: {
  checked: boolean;
  onChange: (on: boolean) => void;
  children: ReactNode;
}) {
  return (
    <label className="flex items-start gap-3 text-body">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="mt-1 size-4 shrink-0 accent-run"
      />
      <span className="flex min-w-0 flex-wrap items-center gap-2">{children}</span>
    </label>
  );
}

/** A value or a list as the list: an empty one is `undefined`, a single one stays a plain string in the file. */
export const listOf = (v: string | string[] | undefined): string[] => (v === undefined ? [] : Array.isArray(v) ? v : [v]);
export const fromList = (list: string[]): string | string[] | undefined =>
  list.length === 0 ? undefined : list.length === 1 ? list[0] : list;

/**
 * Values as chips with a remove button each, and an input that adds one on Enter or a comma (and on blur,
 * so typed text is never lost). The chips are the value; the input is the way to add.
 */
export function ChipList({
  label,
  values,
  onChange,
  prefix,
  placeholder,
  className,
}: {
  label: string;
  values: string[];
  onChange: (values: string[]) => void;
  /** A word before each chip: "conclusion", as the picture has it. */
  prefix?: string;
  placeholder: string;
  className?: string;
}) {
  const id = useId();
  const [text, setText] = useState('');
  const add = () => {
    const next = text
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s && !values.includes(s));
    if (next.length) onChange([...values, ...next]);
    setText('');
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      add();
    } else if (e.key === 'Backspace' && text === '' && values.length) {
      onChange(values.slice(0, -1));
    }
  };
  return (
    <div role="group" aria-label={label} className={cn('flex min-w-0 flex-wrap items-center gap-2', className)}>
      {values.map((value) => (
        <span
          key={value}
          className="inline-flex h-8 max-w-full items-center gap-1 rounded-control bg-run-tint pl-3 pr-1 text-body text-run-ink"
        >
          {prefix ? <span className="text-muted">{prefix}</span> : null}
          <span title={value} className="min-w-0 truncate font-control">
            {value}
          </span>
          <button
            type="button"
            aria-label={`Remove ${value} from ${label.toLowerCase()}`}
            onClick={() => onChange(values.filter((v) => v !== value))}
            className="press grid size-6 place-items-center rounded-chip hover:bg-hover"
          >
            <X size={12} strokeWidth={1.75} aria-hidden="true" />
          </button>
        </span>
      ))}
      <Input
        id={id}
        aria-label={label}
        placeholder={placeholder}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={onKey}
        onBlur={add}
        className="h-8 w-40 rounded-control text-body"
      />
    </div>
  );
}
