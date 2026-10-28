// Replace secret (spec 4 §10): one dialog per secret field. The new value goes to goald once, in
// PATCH …/connections/{name} {secrets}, and is cleared here; goald seals it and never answers
// it. A fresh sign-in is asked first when the session is older than 10 minutes (session.ts).
// SecretReplaceDialog is the same dialog over any save: the Credentials screen replaces a
// credential's value with it (PATCH …/credentials/{name} {value}).
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useRef, useState } from 'react';
import { toast } from 'sonner';
import { updateConnection } from '../../api/connections';
import { keys } from '../../api/queries';
import type { Connection, ConnectionKindField } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { Field } from '../../ui/field';
import { WriteOnly } from './parts';
import { SecretInput } from './secret-input';
import { refusalText } from './words';

/**
 * A dialog that takes one new secret value and hands it to `save` once. The value is cleared when
 * the save succeeds or the dialog closes; `onSaved` then tells the screen what changed (a toast,
 * the cache). The labels are the caller's: "Replace token of github", "Replace value".
 */
export function SecretReplaceDialog<Saved>({
  open,
  title,
  label,
  help,
  actionLabel,
  pendingLabel,
  save,
  onSaved,
  onClose,
}: {
  open: boolean;
  title: string;
  /** The field's name on the form ("API token"). */
  label: string;
  help?: string | undefined;
  /** The submit button's text when idle, and while the save is pending. */
  actionLabel: string;
  pendingLabel: string;
  save: (value: string) => Promise<Saved>;
  onSaved: (saved: Saved) => Promise<void> | void;
  onClose: () => void;
}) {
  const [value, setValue] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  const replace = useMutation({
    // The secret is in mutationFn's closure: forget the mutation once nothing shows it.
    gcTime: 0,
    mutationFn: () => save(value),
    onSuccess: async (saved) => {
      setValue('');
      onClose();
      await onSaved(saved);
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (open && value !== '' && !replace.isPending) replace.mutate();
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) return;
        setValue('');
        replace.reset();
        onClose();
      }}
    >
      <DialogContent
        title={title}
        description="Never shown again, not even to you. It applies from the next run."
        initialFocus={inputRef}
      >
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <Field label={label} aside={<WriteOnly />} help={help}>
            <SecretInput ref={inputRef} value={value} onChange={(e) => setValue(e.target.value)} />
          </Field>
          {replace.error ? (
            <div role="alert">
              <Callout tone="rework">{refusalText(replace.error)}</Callout>
            </div>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary" size="lg" className="md:h-10 md:text-sm">
                Cancel
              </Button>
            </DialogClose>
            <Button
              type="submit"
              variant="primary"
              size="lg"
              className="md:h-10 md:text-sm"
              disabled={value === '' || replace.isPending}
            >
              {replace.isPending ? pendingLabel : actionLabel}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** A connection's secret field: its replace dialog, with the connection's own toast and cache. */
export function ReplaceSecretDialog({
  org,
  connection,
  field,
  onClose,
}: {
  org: string;
  connection: Connection;
  /** The secret field to replace; null closes the dialog. */
  field: ConnectionKindField | null;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const set = connection.secrets_set.includes(field?.name ?? '');
  const verb = set ? 'Replace' : 'Set';
  const fieldTitle = field?.title ?? 'Secret';

  return (
    <SecretReplaceDialog<Connection>
      open={field !== null}
      title={`${verb} ${field?.title.toLowerCase() ?? 'secret'} of ${connection.name}`}
      label={fieldTitle}
      help={field?.description}
      actionLabel={`${verb} ${field?.title.toLowerCase() ?? 'secret'}`}
      pendingLabel={`${verb === 'Set' ? 'Setting' : 'Replacing'}…`}
      save={(value) => updateConnection(org, connection.name, { secrets: { [field?.name ?? '']: value } })}
      onSaved={async (updated) => {
        client.setQueryData(keys.connection(org, updated.name), (old: Connection | undefined) =>
          old ? { ...old, ...updated } : updated,
        );
        toast.success(`${fieldTitle} ${set ? 'replaced' : 'set'} on ${updated.name}`, {
          description: 'Runs get it from their next start. Test it to check it works.',
        });
        await client.invalidateQueries({ queryKey: keys.connections(org) });
      }}
      onClose={onClose}
    />
  );
}
