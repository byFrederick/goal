// What the API key and personal token lists show alike: scope chips ("+N more" past two) and the
// "in 9 days" of an expiry under 14 days.
import { Clock } from 'lucide-react';
import { Chip } from '../../ui/chip';
import { scopeLabel } from './scopes';

/** How many scope chips a row shows before "+N more". */
const chipsShown = 2;

export function Scopes({ scopes }: { scopes: readonly string[] }) {
  const shown = scopes.slice(0, chipsShown);
  const more = scopes.length - shown.length;
  return (
    <span className="flex flex-wrap gap-2">
      {shown.map((s) => (
        <Chip key={s} tone="quiet">
          {scopeLabel(s)}
        </Chip>
      ))}
      {more > 0 ? (
        <Chip tone="plain">
          <span aria-hidden="true">+{more} more</span>
          <span className="sr-only">and {scopes.slice(chipsShown).map(scopeLabel).join(', ')}</span>
        </Chip>
      ) : null}
    </span>
  );
}

export function Soon({ text }: { text: string }) {
  return (
    <span className="ml-1 inline-flex items-center gap-1 text-secondary font-medium text-rework-ink md:ml-0 md:flex">
      <Clock size={13} strokeWidth={2} aria-hidden="true" />
      {text}
    </span>
  );
}
