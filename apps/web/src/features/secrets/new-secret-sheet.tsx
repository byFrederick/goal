// New secret (ADR 0037 decision 14, canvas docs/design/screens/connections/secrets.jpg and
// mobile/secret-new.jpg): a side sheet on the list, a full screen on a phone. A name, the pasted value
// (write-only: there is no source chooser), who may use it and when it expires. The value lives in this
// form's state only: it leaves once, in the create request, and the sheet's content unmounts on close,
// which drops it. The mutation is not kept in the cache (gcTime 0), and the URL holds none of it.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { keys } from '../../api/queries';
import { createSecret } from '../../api/secrets';
import type { Factory, NewSecret } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Drawer, DrawerContent } from '../../ui/drawer';
import { Field, Input } from '../../ui/field';
import { ExpiryFields, expiresAt, ScopeField, type ScopeState, scopeBody, WriteOnly } from './parts';
import { SecretInput } from './secret-input';
import { nameProblem, refusalText } from './words';

export function NewSecretSheet({
  org,
  open,
  onOpenChange,
  factories,
  initial,
  onCreated,
}: {
  org: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  factories: readonly Factory[];
  /** Fills the form: the suggested name, and a value moved out of a file (Move to Secrets). */
  initial?: { name?: string; value?: string; allFactories?: boolean } | undefined;
  /** Called with the secret's name once it is created (Add a tool picks it, Move to Secrets writes its reference). */
  onCreated?: ((name: string) => void) | undefined;
}) {
  return (
    <Drawer open={open} onOpenChange={onOpenChange}>
      <DrawerContent title="New secret">
        <NewSecretForm
          org={org}
          factories={factories}
          initial={initial}
          onCreated={onCreated}
          onDone={() => onOpenChange(false)}
        />
      </DrawerContent>
    </Drawer>
  );
}

function NewSecretForm({
  org,
  factories,
  initial,
  onCreated,
  onDone,
}: {
  org: string;
  factories: readonly Factory[];
  initial: { name?: string; value?: string; allFactories?: boolean } | undefined;
  onCreated: ((name: string) => void) | undefined;
  onDone: () => void;
}) {
  const client = useQueryClient();
  const [name, setName] = useState(initial?.name ?? '');
  const [value, setValue] = useState(initial?.value ?? '');
  const [description, setDescription] = useState('');
  const [scope, setScope] = useState<ScopeState>({ all: initial?.allFactories ?? false, chosen: [] });
  const [expires, setExpires] = useState('');
  const [remind, setRemind] = useState(14);
  const [attempted, setAttempted] = useState(false);

  const create = useMutation({
    mutationFn: (secret: NewSecret) => createSecret(org, secret),
    gcTime: 0,
    onSuccess: async (saved) => {
      onDone();
      onCreated?.(saved.name);
      toast.success(`${saved.name} created`, { description: 'Nobody can read its value back.' });
      await client.invalidateQueries({ queryKey: keys.secrets(org) });
    },
  });

  const trimmed = name.trim();
  const nameError = attempted ? nameProblem(trimmed) : undefined;
  const valueError = attempted && value === '' ? 'Paste the value.' : undefined;

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setAttempted(true);
    if (nameProblem(trimmed) || value === '' || create.isPending) return;
    const at = expiresAt(expires);
    create.mutate({
      name: trimmed,
      value,
      description: description.trim(),
      scope: scopeBody(scope),
      ...(at ? { expires_at: at, remind_days: remind } : {}),
    });
  };

  return (
    <form onSubmit={submit} noValidate className="flex min-w-0 flex-1 flex-col gap-5">
      <Field
        label="Name"
        error={nameError}
        help={
          <>
            Lowercase letters, digits and dashes. Files use it as{' '}
            <code className="rounded-chip bg-hover px-1 font-mono text-meta">{`{{${trimmed || 'name'}}}`}</code>.
          </>
        }
      >
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          onBlur={() => setAttempted((a) => a || name !== '')}
          className="font-mono"
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
          placeholder="sentry-token"
        />
      </Field>

      <Field
        label="Value"
        aside={<WriteOnly />}
        error={valueError}
        help="Encrypted before it is stored. You can replace it later, never read it."
      >
        <SecretInput value={value} onChange={(e) => setValue(e.target.value)} placeholder="Paste the token or key" />
      </Field>

      <Field
        label={
          <>
            Description <span className="font-normal text-muted">optional</span>
          </>
        }
      >
        <Input value={description} onChange={(e) => setDescription(e.target.value)} maxLength={500} />
      </Field>

      <ScopeField value={scope} onChange={setScope} factories={factories} />
      <ExpiryFields expires={expires} remind={remind} onExpires={setExpires} onRemind={setRemind} />

      {create.isError ? (
        <div role="alert">
          <Callout tone="rework">{refusalText(create.error)}</Callout>
        </div>
      ) : null}

      <div className="mt-auto flex flex-col gap-3 border-t border-line pt-4 md:flex-row md:items-center md:justify-between">
        <p className="m-0 order-2 text-secondary text-muted max-md:text-center md:order-1">
          No run gets it until a file names it.
        </p>
        <Button
          type="submit"
          variant="action"
          className="order-1 h-11 max-md:w-full md:order-2 md:h-10"
          busy={create.isPending}
          busyLabel="Adding…"
        >
          Add secret
        </Button>
      </div>
    </form>
  );
}
