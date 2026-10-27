// New webhook: a name (a slug, the one a trigger's `on: webhook.<name>` names) and a kind, generic or
// GitHub. Creating shows the signing secret once (secret-dialog.tsx). goald checks the name again:
// 409 webhook-name-taken and 400 webhook-invalid are said in the form.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useId, useRef, useState } from 'react';
import { ApiError } from '../../api/client';
import { keys } from '../../api/queries';
import { createWebhookEndpoint, type WebhookKind } from '../../api/webhooks';
import { Button } from '../../ui/button';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Field, Input } from '../../ui/field';
import { SegmentedControl } from '../../ui/segmented-control';
import type { ShownSecret } from './secret-dialog';
import { nameError } from './words';

interface NewWebhookDialogProps {
  org: string;
  factory: string;
  onClose: () => void;
  /** The new endpoint's secret, to show once. The caller keeps it only while its dialog is open. */
  onCreated: (shown: ShownSecret) => void;
}

/** Why a create failed, in the dialog's words. */
function failure(err: Error): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. The webhook is not created; try again once it is.';
  if (err.code === 'webhook-name-taken') return 'Another webhook in this factory has this name.';
  if (err.code === 'webhook-invalid') return 'goald does not accept this name or kind. Check both and try again.';
  return `Not created: ${err.detail ?? err.title}`;
}

export function NewWebhookDialog({ org, factory, onClose, onCreated }: NewWebhookDialogProps) {
  const [name, setName] = useState('');
  const [kind, setKind] = useState<WebhookKind>('generic');
  const [submitted, setSubmitted] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  const ids = useId();

  const queryClient = useQueryClient();
  const create = useMutation({
    // The secret goes to the page's state, not into the mutation's result, which the mutation cache keeps.
    mutationFn: async () => {
      const created = await createWebhookEndpoint(org, factory, { name: name.trim(), kind });
      onCreated({ endpointName: created.name, secret: created.secret, reason: 'created' });
    },
    // The list is read again, never written from the answer: the secret must not reach the cache.
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.webhooks(org, factory) }),
  });

  const error = submitted ? nameError(name) : undefined;
  const taken = create.error instanceof ApiError && create.error.code === 'webhook-name-taken';

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (nameError(name)) {
      nameRef.current?.focus();
      return;
    }
    if (!create.isPending) create.mutate();
  };

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent
        title="New webhook"
        description="An address other systems post to. A trigger feeds from it."
        initialFocus={nameRef}
        className="max-w-[560px]"
      >
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-5">
          <Field
            label="Name"
            error={taken ? 'Another webhook in this factory has this name.' : error}
            help="Lower-case letters, digits and hyphens. A trigger refers to it as webhook.<name>."
          >
            <Input
              ref={nameRef}
              value={name}
              onChange={(e) => {
                create.reset();
                setName(e.target.value);
              }}
              maxLength={80}
              autoComplete="off"
              spellCheck={false}
              placeholder="cf-deploys"
              aria-describedby={`${ids}-kind`}
            />
          </Field>
          <div className="flex flex-col gap-2">
            <span id={`${ids}-kind`} className="text-body font-medium">
              Kind
            </span>
            <SegmentedControl<WebhookKind>
              label="Kind"
              options={[
                { value: 'generic', label: 'Generic' },
                { value: 'github', label: 'GitHub' },
              ]}
              value={kind}
              onChange={setKind}
            />
            <p className="m-0 text-meta text-muted">
              Generic senders sign with X-Goal-Signature. GitHub senders sign with X-Hub-Signature-256.
            </p>
          </div>
          {create.error && !taken ? (
            <p role="alert" className="m-0 text-secondary text-rework-ink">
              {failure(create.error)}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={create.isPending}>
              {create.isPending ? 'Creating…' : 'Create webhook'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
