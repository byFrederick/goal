// New key (canvas "Api-Keys-New"): the name (the audit log's "<name> (API key)"), when it
// expires (30, 60 or 90 days), the factories it reaches (all, now and later, or only these), the
// most it acts as (viewer, approver, editor) and what it can do, grouped Read / Work / Build with
// presets. A scope above the chosen ceiling is off and says which role it needs; lowering the
// ceiling drops it. goald checks it all again (scope-not-allowed, api-key-name-taken), and
// creating needs a sign-in from the last 10 minutes (the re-auth dialog, then once more).
import { useMutation } from '@tanstack/react-query';
import { type FormEvent, useId, useRef, useState } from 'react';
import { ApiError } from '../../api/client';
import { createApiKey } from '../../api/keys';
import type { CreatedApiKey, Factory, FactoryRole, OrgSummary } from '../../api/types';
import { formatDay } from '../../shared/format';
import {
  allows,
  ceilings,
  type ExpiryDays,
  emptyKeyForm,
  expiryDays,
  type KeyForm,
  keyFormErrors,
  needsText,
  newKeyBody,
  type PresetId,
  presetOf,
  presets,
  scopeGroups,
  withinCeiling,
} from '../../shared/tokens/scopes';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Field, Input } from '../../ui/field';
import { SegmentedControl } from '../../ui/segmented-control';

interface NewKeyDialogProps {
  org: OrgSummary;
  factories: readonly Factory[];
  onClose: () => void;
  onCreated: (created: CreatedApiKey) => void;
}

const radio =
  'm-0 size-5 shrink-0 cursor-pointer appearance-none rounded-full border border-rule bg-card transition-[border-width,border-color] duration-200 checked:border-[6px] checked:border-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run';
const checkbox = 'size-[18px] shrink-0 cursor-pointer accent-ink disabled:cursor-not-allowed';
const legend = 'mb-2 p-0 text-body font-control';

/** Why a create failed, in the dialog's words. */
function failure(err: Error): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Your key is not created; try again once it is.';
  return `Not created: ${err.detail ?? err.title}`;
}

export function NewKeyDialog({ org, factories, onClose, onCreated }: NewKeyDialogProps) {
  const [form, setForm] = useState<KeyForm>(emptyKeyForm);
  const [submitted, setSubmitted] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  const ids = useId();

  const create = useMutation({
    mutationFn: () => createApiKey(org.id, newKeyBody(form)),
    onSuccess: onCreated,
  });

  const errors = keyFormErrors(form);
  const taken = create.error instanceof ApiError && create.error.code === 'api-key-name-taken';
  const nameError = submitted ? errors.name : undefined;
  const set = (patch: Partial<KeyForm>) => {
    if (taken && patch.name !== undefined) create.reset();
    setForm((f) => ({ ...f, ...patch }));
  };
  const setCeiling = (ceiling: FactoryRole) => set({ ceiling, scopes: withinCeiling(form.scopes, ceiling) });
  const toggle = (id: string, on: boolean) =>
    set({ scopes: on ? [...form.scopes, id] : form.scopes.filter((s) => s !== id) });
  const preset = presetOf(form.ceiling, form.scopes);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setSubmitted(true);
    if (Object.keys(errors).length > 0) {
      if (errors.name) nameRef.current?.focus();
      return;
    }
    if (!create.isPending) create.mutate();
  };

  const expires = formatDay(new Date(Date.now() + form.days * 86_400_000).toISOString());
  const restricted = form.factories !== null;

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent title="New key" initialFocus={nameRef} className="max-w-[680px]">
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-5">
          <Field
            label="Name"
            error={taken ? 'Another live key has this name' : nameError}
            help={`The audit log shows its actions as ${form.name.trim() || 'its name'} (API key).`}
          >
            <Input
              ref={nameRef}
              value={form.name}
              onChange={(e) => set({ name: e.target.value })}
              maxLength={80}
              autoComplete="off"
              spellCheck={false}
              placeholder="release-pipeline"
            />
          </Field>

          <div className="grid gap-5 md:grid-cols-2">
            <div className="flex min-w-0 flex-col gap-2">
              <p id={`${ids}-expires`} className="m-0 text-body font-control">
                Expires in
              </p>
              <SegmentedControl<`${ExpiryDays}`>
                label="Expires in"
                options={expiryDays.map((d) => ({ value: `${d}` as const, label: `${d} days` }))}
                value={`${form.days}`}
                onChange={(v) => set({ days: Number(v) as ExpiryDays })}
                describedBy={`${ids}-expires-note`}
                className="self-start"
              />
              <p id={`${ids}-expires-note`} className="m-0 text-secondary text-muted">
                Expires {expires}. No key lasts longer than 90 days.
              </p>
            </div>

            <fieldset className="m-0 min-w-0 border-0 p-0">
              <legend className={legend}>Factories</legend>
              <div className="flex flex-col gap-1">
                <label className="flex min-h-11 cursor-pointer items-center gap-3 text-body md:min-h-8">
                  <input
                    type="radio"
                    name={`${ids}-reach`}
                    checked={!restricted}
                    onChange={() => set({ factories: null })}
                    className={radio}
                  />
                  All factories, now and later
                </label>
                <label className="flex min-h-11 cursor-pointer items-center gap-3 text-body md:min-h-8">
                  <input
                    type="radio"
                    name={`${ids}-reach`}
                    checked={restricted}
                    onChange={() => set({ factories: [] })}
                    className={radio}
                  />
                  Only these
                </label>
                {restricted ? (
                  <div className="flex flex-wrap gap-x-5 gap-y-1 pl-8">
                    {factories.map((f) => (
                      <label
                        key={f.id}
                        className="flex min-h-11 cursor-pointer items-center gap-2 text-body md:min-h-8"
                      >
                        <input
                          type="checkbox"
                          className={checkbox}
                          checked={form.factories?.includes(f.id) ?? false}
                          onChange={(e) =>
                            set({
                              factories: e.target.checked
                                ? [...(form.factories ?? []), f.id]
                                : (form.factories ?? []).filter((x) => x !== f.id),
                            })
                          }
                        />
                        {f.name}
                      </label>
                    ))}
                  </div>
                ) : null}
                {submitted && errors.factories ? (
                  <p className="m-0 pl-8 text-secondary text-rework-ink">{errors.factories}</p>
                ) : null}
              </div>
            </fieldset>
          </div>

          <fieldset className="m-0 min-w-0 border-0 p-0">
            <legend className={legend}>Acts at most as</legend>
            <div className="grid gap-2 sm:grid-cols-3">
              {ceilings.map((c) => (
                <label
                  key={c.value}
                  className={cn(
                    'flex cursor-pointer items-start gap-3 rounded-control border border-line bg-card p-3 transition-colors duration-200 hover:border-rule',
                    form.ceiling === c.value && 'border-ink shadow-[inset_0_0_0_1px_var(--color-ink)]',
                  )}
                >
                  <input
                    type="radio"
                    name={`${ids}-ceiling`}
                    checked={form.ceiling === c.value}
                    onChange={() => setCeiling(c.value)}
                    className={radio}
                  />
                  <span className="flex min-w-0 flex-col">
                    <span className="text-body font-control">{c.label}</span>
                    <span className="text-secondary leading-snug text-muted">{c.description}</span>
                  </span>
                </label>
              ))}
            </div>
          </fieldset>

          <div className="flex min-w-0 flex-col">
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
              <p className="m-0 text-body font-control" aria-hidden="true">
                What it can do
              </p>
              <SegmentedControl<PresetId>
                label="Presets"
                options={presets.map((p) => ({ value: p.id, label: p.label }))}
                value={preset}
                onChange={(id) => {
                  const p = presets.find((x) => x.id === id);
                  if (p) set({ ceiling: p.ceiling, scopes: [...p.scopes] });
                }}
                className="max-w-full shrink flex-wrap"
              />
            </div>
            <fieldset
              aria-describedby={`${ids}-scopes-note`}
              className="m-0 grid min-w-0 gap-4 rounded-control border border-line bg-shell p-4 sm:grid-cols-3"
            >
              <legend className="sr-only">What it can do</legend>
              {scopeGroups.map((g) => (
                <fieldset key={g.id} className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
                  <legend className="mb-2 p-0 text-body font-control">{g.label}</legend>
                  {g.scopes.map((s) => {
                    const off = needsText(form.ceiling, s);
                    const noteId = `${ids}-${s.id}-needs`;
                    return (
                      <label
                        key={s.id}
                        className={cn(
                          'flex min-h-11 cursor-pointer items-start gap-2 text-body md:min-h-0',
                          off && 'cursor-not-allowed text-muted',
                        )}
                      >
                        <input
                          type="checkbox"
                          className={cn(checkbox, 'mt-1')}
                          checked={form.scopes.includes(s.id) && allows(form.ceiling, s)}
                          disabled={Boolean(off)}
                          aria-describedby={off ? noteId : undefined}
                          onChange={(e) => toggle(s.id, e.target.checked)}
                        />
                        <span className="flex flex-col">
                          {s.label}
                          {off ? (
                            <span id={noteId} className="text-meta text-muted">
                              {off}
                            </span>
                          ) : null}
                        </span>
                      </label>
                    );
                  })}
                </fieldset>
              ))}
            </fieldset>
            <p id={`${ids}-scopes-note`} className="m-0 mt-2 text-secondary text-muted">
              Only what you can do yourself, and never people, keys or ownership.
            </p>
            {submitted && errors.scopes ? (
              <p className="m-0 mt-1 text-secondary text-rework-ink">{errors.scopes}</p>
            ) : null}
          </div>

          <div aria-live="polite">
            {create.error && !taken ? <Callout tone="rework">{failure(create.error)}</Callout> : null}
          </div>

          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="white" size="lg" className="md:h-10" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" size="lg" className="md:h-10" disabled={create.isPending}>
              {create.isPending ? 'Creating…' : 'Create key'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
