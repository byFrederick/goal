// Add a case from a past run: the dialog takes the run, a name and what the case checks, then the API
// writes evals/cases/<name>.yaml into a new draft and publishes it through the eval gate (ADR 0025).
// No picture exists for it, so it is built from the dialog and field components alone. A refusal
// (409: a name taken, a draft open, the gate pending or failing) stays in the dialog, where the person
// can read it and change the form.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useRef, useState } from 'react';
import { addEvalCase } from '../../api/client';
import { defKeys, keys } from '../../api/queries';
import type { EvalCaseFromRun, Factory, OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Field, Input } from '../../ui/field';
import { addCaseFailure, caseNamePattern } from './evals-format';

interface AddCaseDialogProps {
  org: OrgSummary;
  factory: Factory;
  onClose: () => void;
}

interface CaseForm {
  run: string;
  name: string;
  description: string;
  tries: string;
  judge: string;
}

const emptyForm: CaseForm = { run: '', name: '', description: '', tries: '1', judge: '' };

/** The body the API takes: optional fields only when they were filled in. */
function caseBody(form: CaseForm): EvalCaseFromRun {
  const body: EvalCaseFromRun = { run: form.run.trim(), name: form.name.trim() };
  if (form.description.trim() !== '') body.description = form.description.trim();
  if (form.judge.trim() !== '') body.judge = form.judge.trim();
  body.tries = Number(form.tries);
  return body;
}

/** What is wrong with the form, by field; empty when it can be sent. */
function formErrors(form: CaseForm): Partial<Record<keyof CaseForm, string>> {
  const errors: Partial<Record<keyof CaseForm, string>> = {};
  if (form.run.trim() === '') errors.run = 'Give the id of the past run.';
  if (!caseNamePattern.test(form.name.trim())) {
    errors.name = 'Use 3 to 40 lowercase letters, digits and hyphens.';
  }
  const tries = Number(form.tries);
  if (!Number.isInteger(tries) || tries < 1 || tries > 10) errors.tries = 'Use a whole number from 1 to 10.';
  return errors;
}

export function AddCaseDialog({ org, factory, onClose }: AddCaseDialogProps) {
  const [form, setForm] = useState<CaseForm>(emptyForm);
  const [submitted, setSubmitted] = useState(false);
  const runRef = useRef<HTMLInputElement>(null);
  const queryClient = useQueryClient();

  const add = useMutation({
    mutationFn: () => addEvalCase(org.id, factory.id, caseBody(form)),
    onSuccess: () => {
      // The new version and its draft are what the publish changed; the cases and their estimate with them.
      for (const key of [
        keys.evals(org.id, factory.id),
        defKeys.versions(org.id, factory.id),
        defKeys.draft(org.id, factory.id),
      ]) {
        void queryClient.invalidateQueries({ queryKey: key });
      }
      onClose();
    },
  });

  const errors = formErrors(form);
  const set = (patch: Partial<CaseForm>) => {
    if (add.error) add.reset();
    setForm((f) => ({ ...f, ...patch }));
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setSubmitted(true);
    if (Object.keys(errors).length > 0) {
      if (errors.run) runRef.current?.focus();
      return;
    }
    if (!add.isPending) add.mutate();
  };

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent title="Add a case from a past run" initialFocus={runRef}>
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
          <Field label="Past run" error={submitted ? errors.run : undefined} help="The run's id, from the Runs page.">
            <Input
              ref={runRef}
              value={form.run}
              onChange={(e) => set({ run: e.target.value })}
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
            />
          </Field>
          <Field
            label="Case name"
            error={submitted ? errors.name : undefined}
            help="It names the file under evals/cases/, such as backend-adds-migration."
          >
            <Input
              value={form.name}
              onChange={(e) => set({ name: e.target.value })}
              maxLength={40}
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
            />
          </Field>
          <Field label="Description" help="What the agent must get right. Empty, the item's title is used.">
            <Input value={form.description} onChange={(e) => set({ description: e.target.value })} maxLength={200} />
          </Field>
          <Field
            label="Tries"
            error={submitted ? errors.tries : undefined}
            help="How many times a run tries the case, from 1 to 10."
          >
            <Input type="number" min={1} max={10} value={form.tries} onChange={(e) => set({ tries: e.target.value })} />
          </Field>
          <Field label="Judge" help="A judge's slug in the current version. Empty, the agent's contract checks it.">
            <Input
              value={form.judge}
              onChange={(e) => set({ judge: e.target.value })}
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
            />
          </Field>

          {add.isError ? (
            <div aria-live="polite">
              <Callout tone="rework">{addCaseFailure(add.error)}</Callout>
            </div>
          ) : null}

          <div className="flex flex-wrap justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={add.isPending}>
              Add case
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
