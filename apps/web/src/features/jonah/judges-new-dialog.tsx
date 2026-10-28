// New judge: a name, the file name it is saved under and what it checks. The judge is published with two
// outcomes (pass and fail) and no rubric yet; the judge page then edits the rubric and the rest. A refusal
// (409: a name taken, a draft open, the gate pending or failing) stays in the dialog.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { type FormEvent, useRef, useState } from 'react';
import { createJudge } from '../../api/client';
import type { CreateJudgeRequest, Factory, OrgSummary } from '../../api/types';
import { useFactoryParams } from '../../shared/factory';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Field, Input } from '../../ui/field';
import { refreshAfterPublish } from './judges-cache';
import { publishFailure } from './judges-format';

/** A judge's file name: 3 to 40 lowercase letters, digits and hyphens, starting with a letter (the contract's rule). */
export const judgeSlugPattern = /^[a-z][a-z0-9-]{2,39}$/;

interface NewJudgeDialogProps {
  org: OrgSummary;
  factory: Factory;
  onClose: () => void;
}

interface NewJudgeForm {
  title: string;
  slug: string;
  criterion: string;
}

const emptyForm: NewJudgeForm = { title: '', slug: '', criterion: '' };

/** The body the API takes: the default outcomes, a pass mark of 0.7 and a sample of 25%, as the picture shows. */
function judgeBody(form: NewJudgeForm): CreateJudgeRequest {
  return {
    slug: form.slug.trim(),
    title: form.title.trim(),
    criterion: form.criterion.trim(),
    outcomes: [
      { score: 1, label: 'Pass', meaning: 'Meets what the judge checks' },
      { score: 0, label: 'Fail', meaning: 'Does not meet it' },
    ],
    pass_mark: 0.7,
    sample: 0.25,
    rubric: '',
  };
}

function formErrors(form: NewJudgeForm): Partial<Record<keyof NewJudgeForm, string>> {
  const errors: Partial<Record<keyof NewJudgeForm, string>> = {};
  if (form.title.trim() === '') errors.title = 'Give the judge a name.';
  if (!judgeSlugPattern.test(form.slug.trim())) {
    errors.slug = 'Use 3 to 40 lowercase letters, digits and hyphens, starting with a letter.';
  }
  if (form.criterion.trim() === '') errors.criterion = 'Say what the judge checks.';
  return errors;
}

export function NewJudgeDialog({ org, factory, onClose }: NewJudgeDialogProps) {
  const [form, setForm] = useState<NewJudgeForm>(emptyForm);
  const [submitted, setSubmitted] = useState(false);
  const titleRef = useRef<HTMLInputElement>(null);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const params = useFactoryParams();

  const create = useMutation({
    mutationFn: () => createJudge(org.id, factory.id, judgeBody(form)),
    onSuccess: (judge) => {
      refreshAfterPublish(queryClient, org.id, factory.id);
      onClose();
      void navigate({
        to: '/o/$org/f/$factory/jonah/judges/$judge',
        params: { ...params, judge: judge.slug },
      });
    },
  });

  const errors = formErrors(form);
  const set = (patch: Partial<NewJudgeForm>) => {
    if (create.error) create.reset();
    setForm((f) => ({ ...f, ...patch }));
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setSubmitted(true);
    if (Object.keys(errors).length > 0) {
      if (errors.title) titleRef.current?.focus();
      return;
    }
    if (!create.isPending) create.mutate();
  };

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent title="New judge" initialFocus={titleRef}>
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
          <Field label="Name" error={submitted ? errors.title : undefined}>
            <Input
              ref={titleRef}
              value={form.title}
              onChange={(e) => set({ title: e.target.value })}
              maxLength={80}
              autoComplete="off"
            />
          </Field>
          <Field
            label="File name"
            error={submitted ? errors.slug : undefined}
            help="The judge's file under evals/judges/, without .md."
          >
            <Input
              value={form.slug}
              onChange={(e) => set({ slug: e.target.value })}
              maxLength={40}
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
            />
          </Field>
          <Field label="What it checks" error={submitted ? errors.criterion : undefined}>
            <Input value={form.criterion} onChange={(e) => set({ criterion: e.target.value })} maxLength={200} />
          </Field>

          {create.isError ? (
            <div aria-live="polite">
              <Callout tone="rework">{publishFailure(create.error)}</Callout>
            </div>
          ) : null}

          <div className="flex flex-wrap justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={create.isPending}>
              New judge
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
