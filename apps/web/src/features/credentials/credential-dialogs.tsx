// A credential's two dialogs: Replace the value (the same dialog the connections use, over a
// credential's PATCH {value}) and Delete, which names the connections that still refer to it when
// goald refuses with 409 credential-in-use.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { deleteCredential, updateCredential } from '../../api/credentials';
import { keys } from '../../api/queries';
import type { Credential } from '../../api/types';
import { Callout } from '../../ui/callout';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { SecretReplaceDialog } from '../connections/replace-secret-dialog';
import { refusalText } from '../connections/words';

/** Replace the value of `credential`, or nothing while `credential` is null. */
export function ReplaceCredentialDialog({
  org,
  credential,
  onClose,
}: {
  org: string;
  credential: Credential | null;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const name = credential?.name ?? '';
  return (
    <SecretReplaceDialog<Credential>
      open={credential !== null}
      title={`Replace the value of ${name}`}
      label="Value"
      actionLabel="Replace value"
      pendingLabel="Replacing…"
      save={(value) => updateCredential(org, name, { value })}
      onSaved={async (saved) => {
        toast.success(`Value replaced on ${saved.name}`, {
          description: 'Runs get it from their next start.',
        });
        await client.invalidateQueries({ queryKey: keys.credentials(org) });
      }}
      onClose={onClose}
    />
  );
}

/**
 * Delete a credential. A refusal (409 credential-in-use) stays in the dialog with goald's words,
 * which name the connections that still refer to it as {{name}}.
 */
export function DeleteCredentialDialog({
  org,
  credential,
  onClose,
}: {
  org: string;
  credential: Credential | null;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const remove = useMutation({
    mutationFn: (name: string) => deleteCredential(org, name),
    onSuccess: async (_, name) => {
      onClose();
      toast.success(`${name} deleted`);
      await client.invalidateQueries({ queryKey: keys.credentials(org) });
    },
  });

  const name = credential?.name ?? '';
  return (
    <ConfirmDialog
      open={credential !== null}
      onOpenChange={(open) => {
        if (open) return;
        remove.reset();
        onClose();
      }}
      title={`Delete ${name}?`}
      description="Runs stop getting its value. A connection that refers to it must stop first."
      confirmLabel="Delete credential"
      pendingLabel="Deleting…"
      tone="danger"
      pending={remove.isPending}
      onConfirm={() => {
        if (credential) remove.mutate(credential.name);
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
