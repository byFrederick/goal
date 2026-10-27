// New token (spec 3 §10.2 "Personal tokens"): the name, when it expires (7, 30, 60 or 90 days,
// 30 by default) and what it can do, grouped Read / Work / High impact, with one preset, "CLI:
// work", that never includes a high-impact scope. A scope the person does not hold now is off
// (goald would refuse it). The dialog starts empty every time and reads nothing from the URL, so
// a link cannot set a token up for someone else's use. goald checks it all again
// (scope-not-allowed, personal-token-name-taken), and creating needs a sign-in from the last 10
// minutes in this browser (the re-auth dialog, then once more).
import { useMutation } from '@tanstack/react-query';
import { type FormEvent, useId, useRef, useState } from 'react';
import { ApiError } from '../../api/client';
import { createPersonalToken } from '../../api/keys';
import type { CreatedPersonalToken, Factory, OrgSummary } from '../../api/types';
import { formatDay } from '../../shared/format';
import {
  cliWorkPreset,
  emptyTokenForm,
  holdsScope,
  newTokenBody,
  type TokenExpiryDays,
  type TokenForm,
  tokenExpiryDays,
  tokenFormErrors,
  tokenScopeGroups,
} from '../../shared/tokens/tokens';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Field, Input } from '../../ui/field';
import { SegmentedControl } from '../../ui/segmented-control';

interface NewTokenDialogProps {
  org: OrgSummary;
  /** The organization's `can`: what the person holds in it. */
  orgCan: readonly string[] | undefined;
  factories: readonly Factory[];
  onClose: () => void;
  onCreated: (created: CreatedPersonalToken) => void;
}

const checkbox = 'size-[18px] shrink-0 cursor-pointer accent-ink disabled:cursor-not-allowed';

/** Why a create failed, in the dialog's words. */
function failure(err: Error): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Your token is not created; try again once it is.';
  if (err.code === 'browser-session-required') return 'Not created: personal tokens are created in the browser only.';
  return `Not created: ${err.detail ?? err.title}`;
}

export function NewTokenDialog({ org, orgCan, factories, onClose, onCreated }: NewTokenDialogProps) {
  const [form, setForm] = useState<TokenForm>(emptyTokenForm);
  const [submitted, setSubmitted] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  const ids = useId();

  const create = useMutation({
    mutationFn: () => createPersonalToken(org.id, newTokenBody(form)),
    onSuccess: onCreated,
  });

  const errors = tokenFormErrors(form);
  const taken = create.error instanceof ApiError && create.error.code === 'personal-token-name-taken';
  const nameError = submitted ? errors.name : undefined;
  const set = (patch: Partial<TokenForm>) => {
    if (taken && patch.name !== undefined) create.reset();
    setForm((f) => ({ ...f, ...patch }));
  };
  const toggle = (id: string, on: boolean) =>
    set({ scopes: on ? [...form.scopes, id] : form.scopes.filter((s) => s !== id) });
  const held = (s: (typeof tokenScopeGroups)[number]['scopes'][number]) => holdsScope(s, orgCan, factories);

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
  // The preset, less what the person's role does not allow.
  const cliWork = tokenScopeGroups
    .flatMap((g) => g.scopes)
    .filter((s) => cliWorkPreset.includes(s.id) && held(s))
    .map((s) => s.id);
  const presetOn = form.scopes.length > 0 && [...form.scopes].sort().join() === [...cliWork].sort().join();

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent title="New token" initialFocus={nameRef} className="max-w-[680px]">
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-5">
          <Field
            label="Name"
            error={taken ? 'Another live token of yours has this name' : nameError}
            help="Name it after where it lives, such as laptop or build-box."
          >
            <Input
              ref={nameRef}
              value={form.name}
              onChange={(e) => set({ name: e.target.value })}
              maxLength={80}
              autoComplete="off"
              spellCheck={false}
              placeholder="laptop"
            />
          </Field>

          <div className="flex min-w-0 flex-col gap-2">
            <p className="m-0 text-sm font-medium" aria-hidden="true">
              Expires in
            </p>
            <SegmentedControl<`${TokenExpiryDays}`>
              label="Expires in"
              options={tokenExpiryDays.map((d) => ({ value: `${d}` as const, label: `${d} days` }))}
              value={`${form.days}`}
              onChange={(v) => set({ days: Number(v) as TokenExpiryDays })}
              describedBy={`${ids}-expires-note`}
              className="max-w-full self-start"
            />
            <p id={`${ids}-expires-note`} className="m-0 text-secondary text-muted">
              Expires {expires}. No token lasts longer than 90 days.
            </p>
          </div>

          <div className="flex min-w-0 flex-col">
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
              <p className="m-0 text-sm font-medium" aria-hidden="true">
                What it can do
              </p>
              <Button
                variant="white"
                size="sm"
                className="h-11 md:h-8"
                aria-pressed={presetOn}
                onClick={() => set({ scopes: [...cliWork] })}
              >
                CLI: work
              </Button>
            </div>
            <fieldset
              aria-describedby={`${ids}-scopes-note`}
              className="m-0 grid min-w-0 gap-4 rounded-control border border-line bg-shell p-4 sm:grid-cols-3"
            >
              <legend className="sr-only">What it can do</legend>
              {tokenScopeGroups.map((g) => (
                <fieldset
                  key={g.id}
                  aria-describedby={g.id === 'high' ? `${ids}-high` : undefined}
                  className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0"
                >
                  <legend className="mb-2 p-0 text-sm font-medium">{g.label}</legend>
                  {g.scopes.map((s) => {
                    const off = !held(s);
                    const noteId = `${ids}-${s.id}-off`;
                    return (
                      <label
                        key={s.id}
                        className={cn(
                          'flex min-h-11 cursor-pointer items-start gap-3 text-body md:min-h-0',
                          off && 'cursor-not-allowed text-muted',
                        )}
                      >
                        <input
                          type="checkbox"
                          className={cn(checkbox, 'mt-1')}
                          checked={form.scopes.includes(s.id) && !off}
                          disabled={off}
                          aria-describedby={off ? noteId : undefined}
                          onChange={(e) => toggle(s.id, e.target.checked)}
                        />
                        <span className="flex flex-col">
                          {s.label}
                          {off ? (
                            <span id={noteId} className="text-xs text-muted">
                              Your role doesn’t allow it
                            </span>
                          ) : null}
                        </span>
                      </label>
                    );
                  })}
                  {g.id === 'high' ? (
                    <p id={`${ids}-high`} className="m-0 mt-1 text-secondary leading-snug text-rework-ink">
                      Publishing a version controls the prompts agents run and the repository they push to. Give this
                      only to a token you keep safe.
                    </p>
                  ) : null}
                </fieldset>
              ))}
            </fieldset>
            <p id={`${ids}-scopes-note`} className="m-0 mt-2 text-secondary text-muted">
              The token acts as you: never more than your role allows at each request, and never people, keys or
              ownership.
            </p>
            {submitted && errors.scopes ? (
              <p className="m-0 mt-1 text-secondary text-rework-ink">{errors.scopes}</p>
            ) : null}
          </div>

          <div aria-live="polite">
            {create.error && !taken ? <Callout tone="rework">{failure(create.error)}</Callout> : null}
          </div>

          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="white" size="lg" className="md:h-10 md:text-sm" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="lg"
              className="md:h-10 md:text-sm"
              disabled={create.isPending}
            >
              {create.isPending ? 'Creating…' : 'Create token'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
