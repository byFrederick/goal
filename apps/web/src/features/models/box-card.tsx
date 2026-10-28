// "Box sizes" and the box time limit (canvas "Models and policies"). The three sizes are the install's, read
// only (ADR 0024): each shows its vCPU, memory and the factories that run on it. The limit is the policy's
// box_time_limit_minutes, shown in hours. The hours field keeps the minutes it was loaded with, so a limit
// that is not a whole number of hours round-trips without loss.
import { useEffect, useState } from 'react';
import type { OrgPolicyView } from '../../api/types';
import { SectionCard } from '../../ui/card';
import { Input } from '../../ui/field';
import { gigabytes, hoursOf, minutesOf, type PolicyForm } from './form';

const sizeName = { small: 'Small', medium: 'Medium', large: 'Large' } as const;

export function BoxCard({
  form,
  sizes,
  onChange,
}: {
  form: PolicyForm;
  sizes: OrgPolicyView['box_sizes'];
  onChange: (next: PolicyForm) => void;
}) {
  return (
    <SectionCard title="Box sizes" aside={<span className="text-secondary text-muted">Each run gets a fresh box</span>}>
      <ul className="m-0 grid list-none gap-2 p-0 sm:grid-cols-3">
        {sizes.map((box) => (
          <li key={box.size} className="flex flex-col gap-1 rounded-cell border border-rule p-3">
            <span className="font-medium text-ink">{sizeName[box.size]}</span>
            <span className="text-secondary text-ink-soft">
              {box.vcpu} vCPU, {gigabytes(box.memory_bytes)} GB
            </span>
            <span className="text-meta text-muted">
              {box.factories.length === 0 ? 'Not used' : box.factories.map((f) => f.name).join(', ')}
            </span>
          </li>
        ))}
      </ul>
      <TimeLimit
        minutes={form.box_time_limit_minutes}
        onMinutes={(box_time_limit_minutes) => onChange({ ...form, box_time_limit_minutes })}
      />
    </SectionCard>
  );
}

/**
 * The stop-after rule. Its checkbox is always ticked: the API has no box without a limit, so the
 * rule cannot be switched off. The hours field edits the limit; a value that is not a number above zero
 * is shown as invalid and leaves the limit as it was.
 */
function TimeLimit({ minutes, onMinutes }: { minutes: number; onMinutes: (minutes: number) => void }) {
  const [text, setText] = useState(() => hoursOf(minutes));
  // The minutes this field last wrote or was given: a change from outside (a save or a reload) shows here.
  const [seen, setSeen] = useState(minutes);
  useEffect(() => {
    if (minutes !== seen) {
      setSeen(minutes);
      setText(hoursOf(minutes));
    }
  }, [minutes, seen]);

  const parsed = minutesOf(text);
  const invalid = parsed === null;

  return (
    <div className="flex flex-col gap-2">
      <label className="flex flex-wrap items-center gap-x-2 gap-y-1 text-body text-ink">
        <input type="checkbox" checked disabled readOnly className="size-4 accent-run" />
        <span>Stop a box after</span>
        <Input
          aria-label="Hours before a box stops"
          inputMode="decimal"
          value={text}
          aria-invalid={invalid || undefined}
          aria-describedby={invalid ? 'box-limit-error' : undefined}
          onChange={(e) => {
            setText(e.target.value);
            const next = minutesOf(e.target.value);
            if (next !== null) {
              setSeen(next);
              onMinutes(next);
            }
          }}
          className="h-9 w-20 md:h-8"
        />
        <span>hours, whatever the run is doing</span>
      </label>
      {invalid ? (
        <p id="box-limit-error" role="alert" className="m-0 text-secondary text-rework-ink">
          Enter a number of hours above zero.
        </p>
      ) : null}
    </div>
  );
}
