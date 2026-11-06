// Edit openings (ADR 0040 decision 12): open the webhook to a factory, close it, limit an opening to
// repositories, and rename it. One request changes them together. A rename or a close that would leave
// published triggers without the webhook is refused with 409 webhook-in-use: the dialog lists each trigger
// with its factory, and says to publish them without it first.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useRef, useState } from 'react';
import { ApiError } from '../../api/client';
import { keys } from '../../api/queries';
import type { Factory } from '../../api/types';
import { patchWebhook, type Webhook, type WebhookPatch, type WebhookTrigger } from '../../api/webhooks';
import { factoryTitle } from '../../shared/factory';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Checkbox } from '../../ui/checkbox';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Field, Input } from '../../ui/field';
import { nameProblem } from './words';

/** "acme/api, acme/web" as the list the API takes; an empty text is every repository. */
export function parseRepositories(text: string): string[] {
  return text
    .split(/[\s,]+/)
    .map((r) => r.trim())
    .filter(Boolean);
}

const sameList = (a: readonly string[], b: readonly string[]) => a.length === b.length && a.every((x, i) => x === b[i]);

/** The request that turns the webhook's openings and name into what the form says; undefined when nothing changed. */
export function patchFor(
  webhook: Webhook,
  name: string,
  openings: Record<string, { open: boolean; repositories: string }>,
): WebhookPatch | undefined {
  const patch: WebhookPatch = {};
  if (name !== webhook.name) patch.name = name;
  const open: NonNullable<WebhookPatch['open']> = [];
  const close: string[] = [];
  for (const [factory, now] of Object.entries(openings)) {
    const before = webhook.openings.find((o) => o.factory === factory);
    if (!now.open) {
      if (before) close.push(factory);
      continue;
    }
    const repositories = parseRepositories(now.repositories);
    if (before && sameList(before.repositories ?? [], repositories)) continue;
    open.push({ factory, repositories: repositories.length === 0 ? null : repositories });
  }
  if (open.length > 0) patch.open = open;
  if (close.length > 0) patch.close = close;
  return Object.keys(patch).length === 0 ? undefined : patch;
}

/** The triggers a 409 webhook-in-use names, from the problem. */
export function inUseTriggers(error: unknown): WebhookTrigger[] | undefined {
  if (!(error instanceof ApiError) || error.code !== 'webhook-in-use') return undefined;
  return error.problem.triggers ?? [];
}

export function TriggersInUse({
  triggers,
  factories,
}: {
  triggers: readonly WebhookTrigger[];
  factories: readonly Factory[];
}) {
  return (
    <div role="alert" className="flex flex-col gap-2">
      <Callout tone="rework">
        Published triggers still use this webhook. Publish them without it first, then try again.
      </Callout>
      <ul className="m-0 flex list-none flex-col gap-1 p-0 text-secondary">
        {triggers.map((t) => (
          <li key={`${t.factory}/${t.trigger}`} className="flex flex-wrap gap-x-2">
            <span className="font-medium text-ink">{t.trigger}</span>
            <span className="text-muted">
              {factoryTitle(factories.find((f) => f.id === t.factory) ?? { name: t.factory_name })}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function EditWebhookDialog({
  org,
  webhook,
  factories,
  onClose,
  onRenamed,
}: {
  org: string;
  webhook: Webhook;
  factories: readonly Factory[];
  onClose: () => void;
  /** The webhook's new name, once the rename is saved: its page moves to it. */
  onRenamed: (name: string) => void;
}) {
  const client = useQueryClient();
  const nameRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState(webhook.name);
  const [attempted, setAttempted] = useState(false);
  const [openings, setOpenings] = useState(() =>
    Object.fromEntries(
      factories.map((f) => {
        const current = webhook.openings.find((o) => o.factory === f.id);
        return [f.id, { open: current !== undefined, repositories: (current?.repositories ?? []).join(', ') }];
      }),
    ),
  );

  const save = useMutation({
    mutationFn: (patch: WebhookPatch) => patchWebhook(org, webhook.name, patch),
    gcTime: 0,
    onSuccess: async (saved) => {
      await client.invalidateQueries({ queryKey: keys.webhooks(org) });
      onClose();
      if (saved.name !== webhook.name) onRenamed(saved.name);
    },
  });

  const nameError = attempted && name !== webhook.name ? nameProblem(name) : undefined;
  const taken = save.error instanceof ApiError && save.error.code === 'webhook-name-taken';
  const triggers = inUseTriggers(save.error);
  const refused =
    save.error instanceof ApiError && save.error.code === 'webhook-invalid' ? save.error.detail : undefined;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setAttempted(true);
    if (name !== webhook.name && nameProblem(name)) {
      nameRef.current?.focus();
      return;
    }
    const patch = patchFor(webhook, name, openings);
    if (!patch) return onClose();
    if (!save.isPending) save.mutate(patch);
  };

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent
        title="Edit openings"
        description={`Which factories receive ${webhook.name}, and from which repositories.`}
        className="max-w-[560px]"
      >
        <form onSubmit={submit} noValidate className="flex flex-col gap-5">
          <Field
            label="Name"
            error={taken ? 'Another webhook of the organization has this name.' : nameError}
            help={
              name !== webhook.name
                ? 'Renaming changes what triggers write: they must name the webhook by its new name.'
                : undefined
            }
          >
            <Input
              ref={nameRef}
              value={name}
              onChange={(e) => {
                save.reset();
                setName(e.target.value);
              }}
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
            />
          </Field>
          <fieldset className="m-0 flex min-w-0 flex-col gap-3 border-0 p-0">
            <legend className="mb-1 p-0 text-sm font-medium">Open to</legend>
            {factories.map((f) => {
              const row = openings[f.id] ?? { open: false, repositories: '' };
              return (
                <div
                  key={f.id}
                  className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] items-center gap-3 max-sm:grid-cols-1"
                >
                  <Checkbox
                    label={factoryTitle(f)}
                    checked={row.open}
                    onCheckedChange={(open) => {
                      save.reset();
                      setOpenings((o) => ({ ...o, [f.id]: { ...row, open } }));
                    }}
                  />
                  {row.open ? (
                    <Input
                      aria-label={`Repositories for ${factoryTitle(f)}`}
                      placeholder="All repositories"
                      value={row.repositories}
                      autoComplete="off"
                      spellCheck={false}
                      className="font-mono"
                      onChange={(e) => {
                        save.reset();
                        setOpenings((o) => ({ ...o, [f.id]: { ...row, repositories: e.target.value } }));
                      }}
                    />
                  ) : null}
                </div>
              );
            })}
            <p className="m-0 text-meta text-muted">
              Limit an opening with owner/name, separated by commas. Empty is every repository.
            </p>
          </fieldset>
          {triggers ? <TriggersInUse triggers={triggers} factories={factories} /> : null}
          {refused ? (
            <div role="alert">
              <Callout tone="rework">{refused}</Callout>
            </div>
          ) : null}
          {save.isError && !triggers && !taken && !refused ? (
            <div role="alert">
              <Callout tone="rework">Not saved. Check your connection and try again.</Callout>
            </div>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" busy={save.isPending} busyLabel="Saving…">
              Save
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
