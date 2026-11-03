// A secret's two dialogs: Replace the value (the dialog the connections use, over PUT …/value) and
// Delete, which keeps goald's words in the dialog when it refuses with 409 secret-in-use.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { keys } from '../../api/queries';
import { deleteSecret, replaceSecretValue } from '../../api/secrets';
import type { Secret } from '../../api/types';
import { Callout } from '../../ui/callout';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { SecretReplaceDialog } from '../connections/replace-secret-dialog';
import { refusalText } from '../connections/words';

/** Replace the value of `secret`, or nothing while `secret` is null. */
export function ReplaceValueDialog({
  org,
  secret,
  onClose,
}: {
  org: string;
  secret: Secret | null;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const name = secret?.name ?? '';
  return (
    <SecretReplaceDialog<Secret>
      open={secret !== null}
      title={`Replace the value of ${name}`}
      label="Value"
      actionLabel="Replace value"
      pendingLabel="Replacing…"
      save={(value) => replaceSecretValue(org, name, value)}
      onSaved={async (saved) => {
        toast.success(`Value replaced on ${saved.name}`, { description: 'Runs that start afterwards get it.' });
        await client.invalidateQueries({ queryKey: keys.secrets(org) });
      }}
      onClose={onClose}
    />
  );
}

/**
 * Delete a secret. Nothing is deleted while something uses it: the page disables the button then,
 * and a refusal that still comes (409 secret-in-use, another admin added a use) stays in the dialog.
 */
export function DeleteSecretDialog({
  org,
  secret,
  onClose,
  onDeleted,
}: {
  org: string;
  secret: Secret | null;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const client = useQueryClient();
  const remove = useMutation({
    mutationFn: (name: string) => deleteSecret(org, name),
    onSuccess: async (_, name) => {
      onClose();
      toast.success(`${name} deleted`);
      client.removeQueries({ queryKey: keys.secret(org, name) });
      await client.invalidateQueries({ queryKey: keys.secrets(org) });
      onDeleted();
    },
  });

  const name = secret?.name ?? '';
  return (
    <ConfirmDialog
      open={secret !== null}
      onOpenChange={(open) => {
        if (open) return;
        remove.reset();
        onClose();
      }}
      title={`Delete ${name}?`}
      description="Its value is destroyed. This can’t be undone."
      confirmLabel="Delete secret"
      pendingLabel="Deleting…"
      tone="danger"
      pending={remove.isPending}
      onConfirm={() => {
        if (secret) remove.mutate(secret.name);
      }}
    >
      {remove.error ? (
        <div role="alert">
          <Callout tone="rework">{refusalText(remove.error)}</Callout>
        </div>
      ) : null}
    </ConfirmDialog>
  );
}
