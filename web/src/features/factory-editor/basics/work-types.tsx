// Work types as a token field (D §10.5): a chip per type with "Remove work type x", and an input
// where Enter or a comma adds the typed type once it is a slug and not listed yet.
import { X } from 'lucide-react';
import { type KeyboardEvent, useId, useState } from 'react';
import type { BasicsSection } from '../../../shared/definition/model';
import { cn } from '../../../ui/cn';
import { addWorkType, removeWorkType } from '../draft/ops';
import { useDraftSession } from '../draft/use-draft';
import { EditedMark } from '../frame/section-shell';
import { useProblemText } from '../frame/use-editor';

const slugRule = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export function WorkTypes({ basics }: { basics: BasicsSection }) {
  const session = useDraftSession();
  const id = useId();
  const [typed, setTyped] = useState('');
  const [error, setError] = useState<string | null>(null);
  const problem = useProblemText('/basics/work_types');
  const types = basics.work_types ?? [];
  const shownError = error ?? problem;

  const add = () => {
    const type = typed.trim();
    if (type === '') return;
    if (!slugRule.test(type)) {
      setError('A work type is lowercase letters, digits and dashes, as in bug-fix.');
      return;
    }
    if (types.includes(type)) {
      setError(`${type} is already a work type.`);
      return;
    }
    session.edit(addWorkType(basics, type));
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
          Work types
        </span>
        <EditedMark section="basics" pointer="/basics/work_types" label="work types" />
      </div>
      <fieldset
        aria-labelledby={`${id}-label`}
        className={cn(
          'm-0 flex min-h-11 flex-wrap items-center gap-1.5 rounded-control border bg-surface px-1.5 py-1 transition-colors duration-200 md:min-h-10',
          // The input inside draws no ring of its own: the whole field shows focus.
          'has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-2 has-[input:focus-visible]:outline-run',
          shownError ? 'border-rework' : 'border-rule hover:border-faint',
        )}
      >
        {types.map((type) => (
          <span
            key={type}
            className="inline-flex h-8 items-center gap-1 rounded-chip border border-line bg-surface pr-1 pl-2.5 text-[13px]"
          >
            {type}
            <button
              type="button"
              aria-label={`Remove work type ${type}`}
              onClick={() => session.edit(removeWorkType(basics, type))}
              className="inline-flex size-6 items-center justify-center rounded-chip text-muted transition-colors duration-200 hover:bg-shell hover:text-ink"
            >
              <X size={14} strokeWidth={1.5} aria-hidden="true" />
            </button>
          </span>
        ))}
        <input
          value={typed}
          onChange={(e) => {
            setTyped(e.target.value);
            setError(null);
          }}
          onKeyDown={onKeyDown}
          onBlur={add}
          aria-label="Add a work type"
          aria-describedby={`${id}-help`}
          aria-invalid={shownError ? true : undefined}
          placeholder="Add a type…"
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
          className="h-8 min-w-[110px] flex-1 border-0 bg-transparent px-1.5 text-base text-ink placeholder:text-muted focus-visible:outline-none md:text-sm"
        />
      </fieldset>
      <div id={`${id}-help`} className="flex flex-col gap-0.5 text-[13px] leading-snug">
        {shownError ? (
          <p role="alert" className="m-0 text-rework-ink">
            {shownError}
          </p>
        ) : null}
        <p className="m-0 text-muted">Items get one when they arrive. Lines can route on them.</p>
      </div>
    </div>
  );
}
