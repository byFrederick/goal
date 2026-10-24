import { CalendarDays, Check, ChevronDown } from 'lucide-react';
import { Popover as P } from 'radix-ui';
import { type FormEvent, useState } from 'react';
import { Button } from './button';
import { cn } from './cn';
import { Field, Input } from './field';

/**
 * A range of days on a white toggle (MASTER.md header controls): the trigger shows the range
 * ("Oct 17 to 23") and opens a panel with the last-N-days presets and a custom from/to form.
 * Dates are ISO days (YYYY-MM-DD); the caller resolves presets and checks nothing else.
 */
export function DateRange({
  label,
  from,
  to,
  today,
  presets,
  active,
  maxDays,
  onPreset,
  onRange,
  className,
}: {
  /** The trigger's text: "Oct 17 to 23". */
  label: string;
  from: string;
  to: string;
  today: string;
  presets: readonly number[];
  /** The preset the range is, or null for a custom range. */
  active: number | null;
  maxDays: number;
  onPreset: (days: number) => void;
  onRange: (from: string, to: string) => void;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState({ from, to });
  const [error, setError] = useState<string>();

  const openChange = (next: boolean) => {
    if (next) {
      setDraft({ from, to });
      setError(undefined);
    }
    setOpen(next);
  };

  const apply = (e: FormEvent) => {
    e.preventDefault();
    const days = (Date.parse(draft.to) - Date.parse(draft.from)) / 86_400_000 + 1;
    if (!draft.from || !draft.to) return setError('Choose both days.');
    if (draft.to > today) return setError('The range cannot end after today.');
    if (days < 1) return setError('The first day comes before the last.');
    if (days > maxDays) return setError(`A range is at most ${maxDays} days.`);
    onRange(draft.from, draft.to);
    setOpen(false);
  };

  return (
    <P.Root open={open} onOpenChange={openChange}>
      <P.Trigger asChild>
        <button
          type="button"
          className={cn(
            'inline-flex h-9 items-center gap-2 rounded-control bg-surface px-3 text-sm text-ink shadow-raise transition-colors duration-200 hover:bg-shell',
            className,
          )}
        >
          <CalendarDays size={16} strokeWidth={1.5} aria-hidden="true" />
          <span className="tabular-nums">{label}</span>
          <ChevronDown size={14} strokeWidth={1.75} aria-hidden="true" />
        </button>
      </P.Trigger>
      <P.Portal>
        <P.Content
          align="end"
          sideOffset={6}
          collisionPadding={8}
          aria-label="Date range"
          className="z-50 flex w-72 max-w-[calc(100vw-16px)] flex-col gap-3 rounded-card border border-rule bg-surface p-2 text-ink shadow-menu outline-none data-[state=open]:animate-[fade-in_150ms_ease-out]"
        >
          <ul className="m-0 flex list-none flex-col p-0">
            {presets.map((days) => (
              <li key={days}>
                <button
                  type="button"
                  aria-pressed={active === days}
                  onClick={() => {
                    onPreset(days);
                    setOpen(false);
                  }}
                  className="flex min-h-11 w-full items-center justify-between gap-3 rounded-control px-2.5 text-left text-[15px] transition-colors duration-200 hover:bg-shell"
                >
                  Last {days} days
                  {active === days ? <Check size={18} strokeWidth={1.75} aria-hidden="true" /> : null}
                </button>
              </li>
            ))}
          </ul>
          <form onSubmit={apply} className="flex flex-col gap-3 border-t border-line px-2.5 pt-3 pb-1.5" noValidate>
            <div className="flex flex-col gap-2.5">
              <Field label="From">
                <Input
                  type="date"
                  value={draft.from}
                  max={draft.to || today}
                  onChange={(e) => setDraft((d) => ({ ...d, from: e.target.value }))}
                />
              </Field>
              <Field label="To">
                <Input
                  type="date"
                  value={draft.to}
                  min={draft.from}
                  max={today}
                  onChange={(e) => setDraft((d) => ({ ...d, to: e.target.value }))}
                />
              </Field>
            </div>
            {error ? (
              <p role="alert" className="m-0 text-[13px] text-rework-ink">
                {error}
              </p>
            ) : null}
            <Button type="submit" variant="primary" className="justify-center">
              Show these days
            </Button>
          </form>
        </P.Content>
      </P.Portal>
    </P.Root>
  );
}
