// Danger zone (canvas "Org-Settings"): Transfer ownership (transfer.tsx) and Delete, both inline.
// Delete is enabled by typing the organization's saved name. goald answers 202 and the organization
// 404s at once; its data is purged after purge_after. The UI then opens `/`, which goes to the next
// organization or says there is none. Owners only: the page leaves out what the caller may not do.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { type FormEvent, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, deleteOrg } from '../../api/client';
import { keys } from '../../api/queries';
import type { OrgSummary } from '../../api/types';
import { formatDay } from '../../shared/format';
import { Button } from '../../ui/button';
import { Input } from '../../ui/field';
import { SettingsRow, SettingsSection } from './card';
import { TransferOwnership } from './transfer';

/** What deleting takes with it, as the row says it: "Stops all 3 factories and deletes …". */
export function deletesText(factories: number): string {
  const what = 'deletes their definitions, items and logs. This can’t be undone.';
  if (factories === 0) return 'Deletes its definitions, items and logs. This can’t be undone.';
  if (factories === 1) return `Stops the factory and ${what}`;
  return `Stops all ${factories} factories and ${what}`;
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
  return (
    <SettingsSection id="danger" title="Danger zone" tone="danger">
      {transfers ? <TransferOwnership org={org} name={name} /> : null}
      {mayDelete ? <DeleteOrganization org={org} name={name} onLeave={onLeave} /> : null}
    </SettingsSection>
  );
}

function DeleteOrganization({ org, name, onLeave }: { org: OrgSummary; name: string; onLeave: () => void }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const [typed, setTyped] = useState('');

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

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (typed === name && !remove.isPending) remove.mutate();
  };

  return (
    <SettingsRow label={`Delete ${name}`} help={deletesText(org.factory_count)}>
      <form onSubmit={onSubmit} noValidate className="flex flex-col gap-2">
        <label htmlFor="delete-confirm" className="text-secondary text-muted">
          Type <b className="font-mono font-semibold text-ink">{name}</b> to confirm
        </label>
        <div className="flex flex-wrap gap-2">
          <Input
            id="delete-confirm"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder={name}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            className="max-w-65"
          />
          <Button type="submit" variant="danger-fill" disabled={typed !== name || remove.isPending}>
            {remove.isPending ? 'Deleting…' : 'Delete organization'}
          </Button>
        </div>
        {error ? (
          <p role="alert" className="m-0 text-secondary text-rework-ink">
            {error}
          </p>
        ) : null}
      </form>
    </SettingsRow>
  );
}
