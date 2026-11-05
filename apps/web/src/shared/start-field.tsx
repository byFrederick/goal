// New item's Start control (ADR 0040 decision 3): Now, Later (a date and a time) or After items (the refs the
// item waits for). The choice becomes the `start` of the request; Now sends none. goald reads a date and time
// without a zone in the person's notification time zone, so the field says so in its ⓘ.
import type { StartRequest } from '../api/types';
import { cn } from '../ui/cn';
import { InfoTip } from '../ui/info-tip';
import { SegmentedControl } from '../ui/segmented-control';

export type StartMode = 'now' | 'later' | 'after';

export interface StartChoice {
  mode: StartMode;
  /** YYYY-MM-DD */
  date: string;
  /** HH:mm */
  time: string;
  /** The refs, separated by commas or spaces. */
  after: string;
}

const pad = (n: number) => String(n).padStart(2, '0');

/** Tomorrow at 09:00, where Later starts. */
export function defaultStart(now = new Date()): StartChoice {
  const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1);
  return {
    mode: 'now',
    date: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`,
    time: '09:00',
    after: '',
  };
}

/** The request's `start` for a choice: none for Now, or what to tell the person is missing. */
export function startOf(choice: StartChoice): { start?: StartRequest; missing?: string } {
  if (choice.mode === 'later') {
    return choice.date && choice.time
      ? { start: { at: `${choice.date}T${choice.time}` } }
      : { missing: 'Pick a date and a time.' };
  }
  if (choice.mode === 'after') {
    const refs = choice.after.split(/[\s,]+/).filter(Boolean);
    return refs.length > 0 ? { start: { after: refs } } : { missing: 'Name the items it waits for, such as goal#2.' };
  }
  return {};
}

const modes: { value: StartMode; label: string }[] = [
  { value: 'now', label: 'Now' },
  { value: 'later', label: 'Later' },
  { value: 'after', label: 'After items' },
];

const input =
  'h-11 w-full rounded-control border border-rule bg-card px-3 text-base text-ink aria-[invalid=true]:border-rework md:h-8';

export function StartField({
  id,
  value,
  onChange,
  error,
}: {
  id: string;
  value: StartChoice;
  onChange: (next: StartChoice) => void;
  /** The problem with the start, said on the field. */
  error?: string | undefined;
}) {
  const set = (patch: Partial<StartChoice>) => onChange({ ...value, ...patch });
  const invalid = Boolean(error);
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex items-center gap-1 text-sm text-muted">
        Start
        <InfoTip about="Start" side="top">
          Later waits in the inbox under Coming up. After items waits until they are done. A date and time are read in
          your notification time zone, or UTC when you have not set one.
        </InfoTip>
      </div>
      <SegmentedControl
        label="Start"
        options={modes}
        value={value.mode}
        onChange={(mode) => set({ mode })}
        describedBy={invalid ? `${id}-start-error` : undefined}
      />
      {value.mode === 'later' ? (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 motion-safe:animate-fade-in">
          <label className="flex flex-col gap-1 text-sm text-muted" htmlFor={`${id}-date`}>
            Date
            <input
              id={`${id}-date`}
              type="date"
              value={value.date}
              onChange={(e) => set({ date: e.target.value })}
              aria-invalid={invalid}
              aria-describedby={invalid ? `${id}-start-error` : undefined}
              className={cn(input)}
            />
          </label>
          <label className="flex flex-col gap-1 text-sm text-muted" htmlFor={`${id}-time`}>
            Time
            <input
              id={`${id}-time`}
              type="time"
              value={value.time}
              onChange={(e) => set({ time: e.target.value })}
              aria-invalid={invalid}
              aria-describedby={invalid ? `${id}-start-error` : undefined}
              className={cn(input)}
            />
          </label>
        </div>
      ) : null}
      {value.mode === 'after' ? (
        <label className="flex flex-col gap-1 text-sm text-muted motion-safe:animate-fade-in" htmlFor={`${id}-after`}>
          Items it waits for
          <input
            id={`${id}-after`}
            value={value.after}
            onChange={(e) => set({ after: e.target.value })}
            placeholder="goal#2, goal#3"
            aria-invalid={invalid}
            aria-describedby={invalid ? `${id}-start-error` : undefined}
            className={cn(input)}
          />
        </label>
      ) : null}
      {error ? (
        <p id={`${id}-start-error`} role="alert" className="m-0 text-sm text-rework-ink">
          {error}
        </p>
      ) : null}
    </div>
  );
}
