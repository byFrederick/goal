// The parts the Secrets screens share: a secret's factory chips, its expiry text, and the two form
// groups the New secret sheet and the secret page's settings both use (the factory scope, and the
// expiry with its reminder).
import { useId } from 'react';
import type { Factory, Secret, SecretScope } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { factoryMark } from '../../shared/marks';
import { Checkbox } from '../../ui/checkbox';
import { Field, Input } from '../../ui/field';
import { Select } from '../../ui/select';
import { type Expiry, remindOptions } from './words';

/** The factories a secret's runs may open: a letter badge each, or "All factories", or "None" for a scope no run reaches. */
export function FactoryChips({ scope, factories }: { scope: Secret['scope']; factories: readonly Factory[] }) {
  if (scope.all_factories) return <span className="text-muted">All</span>;
  const scoped = factories.filter((f) => scope.factories.includes(f.id));
  if (scoped.length === 0) return <span className="text-muted">None</span>;
  return (
    <span className="inline-flex flex-wrap gap-1">
      {scoped.map((f) => (
        <span key={f.id} className="inline-flex items-center">
          <FactoryBadge mark={factoryMark(f, factories)} />
          <span className="sr-only">{f.name}</span>
        </span>
      ))}
    </span>
  );
}

/** The expiry text. Inside the reminder window and past expiry it is the danger colour with a dot, and the words say it too. */
export function ExpiryText({ expiry }: { expiry: Expiry }) {
  switch (expiry.state) {
    case 'never':
      return <span className="text-muted">Never</span>;
    case 'set':
      return <span className="text-muted">{expiry.date}</span>;
    case 'expired':
      return (
        <span className="inline-flex items-center gap-2 font-semibold text-rework-ink">
          <span aria-hidden="true" className="size-1.5 rounded-full bg-rework" />
          Expired {expiry.date}
        </span>
      );
    case 'reminder':
      return (
        <span className="inline-flex items-center gap-2 text-rework-ink">
          <span aria-hidden="true" className="size-1.5 rounded-full bg-rework" />
          In {expiry.days} {expiry.days === 1 ? 'day' : 'days'}
        </span>
      );
  }
}

/** The scope a form edits: every factory, or the chosen ones. Neither reaches no run. */
export interface ScopeState {
  all: boolean;
  chosen: string[];
}

export const scopeOf = (scope: SecretScope): ScopeState => ({ all: scope.all_factories, chosen: [...scope.factories] });
export const scopeBody = (s: ScopeState): SecretScope => ({ all_factories: s.all, factories: s.all ? [] : s.chosen });

/** "Factories that may use it": one toggle per factory, and All factories. 44 px on a phone. */
export function ScopeField({
  value,
  onChange,
  factories,
}: {
  value: ScopeState;
  onChange: (next: ScopeState) => void;
  factories: readonly Factory[];
}) {
  const toggle = (id: string) =>
    onChange({
      ...value,
      chosen: value.chosen.includes(id) ? value.chosen.filter((x) => x !== id) : [...value.chosen, id],
    });
  return (
    <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
      <legend className="mb-2 p-0 text-sm font-medium text-ink">Factories that may use it</legend>
      <div className="flex flex-wrap items-center gap-2">
        {factories.map((f) => {
          const pressed = value.all || value.chosen.includes(f.id);
          return (
            <button
              key={f.id}
              type="button"
              aria-pressed={pressed}
              disabled={value.all}
              onClick={() => toggle(f.id)}
              className="inline-flex h-11 items-center gap-2 rounded-control border border-rule bg-card px-3 text-body font-control text-ink transition-colors duration-(--duration-quick) hover:border-muted aria-pressed:border-ink aria-pressed:text-ink disabled:opacity-60 md:h-9"
            >
              <FactoryBadge mark={factoryMark(f, factories)} />
              {f.title}
            </button>
          );
        })}
      </div>
      <Checkbox
        checked={value.all}
        onCheckedChange={(all) => onChange({ ...value, all })}
        label="All factories, including new ones"
        className="min-h-11 md:min-h-9"
      />
    </fieldset>
  );
}

/** Expires (a date) and Remind me, which needs an expiry. */
export function ExpiryFields({
  expires,
  remind,
  onExpires,
  onRemind,
}: {
  expires: string;
  remind: number;
  onExpires: (next: string) => void;
  onRemind: (next: number) => void;
}) {
  const remindId = useId();
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <Field
        label={
          <>
            Expires <span className="font-normal text-muted">optional</span>
          </>
        }
      >
        <Input type="date" value={expires} onChange={(e) => onExpires(e.target.value)} />
      </Field>
      <div className="flex min-w-0 flex-col gap-2">
        <label htmlFor={remindId} className="text-sm font-medium text-ink">
          Remind me
        </label>
        <Select
          id={remindId}
          value={String(remind)}
          disabled={!expires}
          onValueChange={(next) => onRemind(Number(next))}
          options={remindOptions.map((o) => ({ value: String(o.days), label: o.label }))}
        />
      </div>
    </div>
  );
}

/** The date input's value ("2027-11-02") as the contract's date-time, or undefined for none. */
export const expiresAt = (day: string): string | undefined =>
  day ? new Date(`${day}T00:00:00Z`).toISOString() : undefined;

/** An ISO date-time as the date input's value, "" for none. */
export const dayOf = (iso: string | null): string => (iso ? iso.slice(0, 10) : '');
