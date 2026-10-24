// Danger zone (C §10.9): Delete {Org}, confirmed by typing its name. goald answers 202 and the
// organization 404s at once; its data is purged after purge_after. The UI then opens `/`, which
// goes to the next organization or says there is none. Transfer ownership (spec 3, transfer.tsx)
// sits above it. Owners only: the page leaves out what the caller may not do.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError, deleteOrg } from '../../api/client';
import { keys } from '../../api/queries';
import type { OrgSummary } from '../../api/types';
import { formatDay } from '../../shared/format';
import { Button } from '../../ui/button';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { SettingsSection } from './section';
import { TransferOwnership } from './transfer';

/** What deleting takes with it: "Deletes 3 factories and their history". */
export function deletesText(factories: number): string {
  if (factories === 0) return 'Deletes the organization and its history';
  if (factories === 1) return 'Deletes 1 factory and its history';
  return `Deletes ${factories} factories and their history`;
}

interface DangerZoneProps {
  org: OrgSummary;
  /** The name as saved: what goald compares the confirmation with. */
  name: string;
  /** Called just before the page leaves for `/`, so it does not ask about unsaved changes. */
  onLeave: () => void;
  /** The caller may delete the organization (org:delete). */
  deletes: boolean;
  /** The caller may hand it to an admin (org:transfer, with sign-in on). */
  transfers: boolean;
}

export function DangerZone({ org, name, onLeave, deletes: mayDelete, transfers }: DangerZoneProps) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const deletes = deletesText(org.factory_count);

  const remove = useMutation({
    mutationFn: () => deleteOrg(org.id, name),
    onSuccess: async ({ purge_after }) => {
      onLeave();
      client.setQueryData<OrgSummary[]>(keys.orgs, (orgs) => orgs?.filter((o) => o.id !== org.id));
      toast.success(`Deleted ${name}`, { description: `goald purges its data after ${formatDay(purge_after)}.` });
      await navigate({ to: '/', replace: true });
      client.removeQueries({ queryKey: keys.org(org.id) });
      void client.invalidateQueries({ queryKey: keys.orgs });
    },
  });

  const error = remove.error
    ? remove.error instanceof ApiError
      ? (remove.error.detail ?? remove.error.title)
      : 'goald is not answering. Try again once it is running.'
    : null;

  return (
    <SettingsSection title="Danger zone" lede="These can’t be undone from here.">
      {transfers ? <TransferOwnership org={org} name={name} /> : null}
      {mayDelete ? (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-card border border-line bg-card p-4">
          <p className="m-0 flex min-w-0 flex-col">
            <span className="text-[15px] font-medium">Delete {name}</span>
            <span className="text-sm text-muted">{deletes}. You type the name to confirm.</span>
          </p>
          <Button
            variant="raised"
            className="h-11 border border-line shadow-none md:h-10"
            onClick={() => {
              remove.reset();
              setOpen(true);
            }}
          >
            Delete organization…
          </Button>
        </div>
      ) : null}
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={`Delete ${name}?`}
        description={`${deletes}. Its address stops working at once.`}
        confirmLabel="Delete organization"
        pendingLabel="Deleting…"
        tone="danger"
        confirmText={name}
        pending={remove.isPending}
        onConfirm={() => remove.mutate()}
      >
        {error ? (
          <p role="alert" className="m-0 text-sm text-rework-ink">
            {error}
          </p>
        ) : null}
      </ConfirmDialog>
    </SettingsSection>
  );
}
