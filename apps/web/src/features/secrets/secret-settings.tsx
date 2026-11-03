// The Settings card of a secret page: its factory scope, expiry, reminder and who made it, with Edit for
// owners and admins (secret:manage). Edit turns the card into a form for the description, the scope and
// the expiry; Save sends patchSecret, which replaces what is present.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { keys } from '../../api/queries';
import { patchSecret } from '../../api/secrets';
import type { Factory, Secret } from '../../api/types';
import { formatDay } from '../../shared/format';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Field, Input } from '../../ui/field';
import { refusalText } from '../connections/words';
import { dayOf, ExpiryFields, expiresAt, ScopeField, type ScopeState, scopeBody, scopeOf } from './parts';
import { expiryOf, reminderText } from './words';

const rowClass = 'grid grid-cols-[88px_minmax(0,1fr)] gap-x-4 text-secondary';

function scopeText(secret: Secret, factories: readonly Factory[]): string {
  if (secret.scope.all_factories) return 'All factories, including new ones';
  const names = factories.filter((f) => secret.scope.factories.includes(f.id)).map((f) => f.title);
  return names.length > 0 ? names.join(', ') : 'None, so no run gets it';
}

export function SettingsCard({
  org,
  secret,
  factories,
  manage,
  now,
}: {
  org: string;
  secret: Secret;
  factories: readonly Factory[];
  manage: boolean;
  now: number;
}) {
  const [editing, setEditing] = useState(false);
  const expiry = expiryOf(secret, now);
  return (
    <section
      aria-labelledby="settings-heading"
      className="flex min-w-0 flex-col rounded-card border border-line bg-card"
    >
      <div className="flex min-h-14 items-center justify-between gap-3 border-b border-line px-4 py-2 md:px-5">
        <h2 id="settings-heading" className="m-0 text-heading">
          Settings
        </h2>
        {manage && !editing ? (
          <Button variant="white" className="h-11 md:h-8.5" onClick={() => setEditing(true)}>
            Edit
          </Button>
        ) : null}
      </div>
      {editing ? (
        <SettingsForm org={org} secret={secret} factories={factories} onDone={() => setEditing(false)} />
      ) : (
        <dl className="m-0 flex flex-col gap-3 px-4 py-4 md:px-5">
          <div className={rowClass}>
            <dt className="text-muted">Factories</dt>
            <dd className="m-0 min-w-0 break-words">{scopeText(secret, factories)}</dd>
          </div>
          <div className={rowClass}>
            <dt className="text-muted">Expires</dt>
            <dd className="m-0">{expiry.state === 'never' ? 'Never' : expiry.date}</dd>
          </div>
          <div className={rowClass}>
            <dt className="text-muted">Reminder</dt>
            <dd className="m-0">{reminderText(secret)}</dd>
          </div>
          <div className={rowClass}>
            <dt className="text-muted">Created</dt>
            <dd className="m-0 min-w-0 break-words">
              {formatDay(secret.created_at)} by {secret.created_by.name}
            </dd>
          </div>
        </dl>
      )}
    </section>
  );
}

function SettingsForm({
  org,
  secret,
  factories,
  onDone,
}: {
  org: string;
  secret: Secret;
  factories: readonly Factory[];
  onDone: () => void;
}) {
  const client = useQueryClient();
  const [description, setDescription] = useState(secret.description);
  const [scope, setScope] = useState<ScopeState>(scopeOf(secret.scope));
  const [expires, setExpires] = useState(dayOf(secret.expires_at));
  const [remind, setRemind] = useState(secret.remind_days || 14);

  const save = useMutation({
    mutationFn: () => {
      const at = expiresAt(expires);
      return patchSecret(org, secret.name, {
        description: description.trim(),
        scope: scopeBody(scope),
        expiry: at ? { expires_at: at, remind_days: remind } : {},
      });
    },
    onSuccess: async () => {
      onDone();
      toast.success(`Settings saved on ${secret.name}`);
      await client.invalidateQueries({ queryKey: keys.secrets(org) });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!save.isPending) save.mutate();
  };

  return (
    <form onSubmit={submit} className="flex min-w-0 flex-col gap-4 px-4 py-4 md:px-5">
      <Field label="Description">
        <Input value={description} onChange={(e) => setDescription(e.target.value)} maxLength={500} />
      </Field>
      <ScopeField value={scope} onChange={setScope} factories={factories} />
      <ExpiryFields expires={expires} remind={remind} onExpires={setExpires} onRemind={setRemind} />
      {save.isError ? (
        <div role="alert">
          <Callout tone="rework">{refusalText(save.error)}</Callout>
        </div>
      ) : null}
      <div className="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="secondary" className="h-11 md:h-10" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" className="h-11 md:h-10" disabled={save.isPending}>
          {save.isPending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </form>
  );
}
