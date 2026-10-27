// Transfer ownership (spec 3 §10.2; canvas "Org-Settings", Danger zone): an owner picks one of the
// organization's admins in the row and presses Transfer; the dialog then asks for the organization's
// name, as before. In one change the admin becomes an owner and the owner an admin. Without an admin
// there is nobody to hand it to: the row says so and links to Members and roles. Needs a sign-in from
// the last 10 minutes (the re-auth dialog asks).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useRouter } from '@tanstack/react-router';
import { type FormEvent, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, transferOwnership } from '../../api/client';
import { keys, membersQuery } from '../../api/queries';
import type { OrgSummary } from '../../api/types';
import { displayName } from '../../shared/names';
import { Button } from '../../ui/button';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { Input, NativeSelect } from '../../ui/field';
import { SettingsRow } from './card';

/** Why a transfer failed, in the dialog's words. */
function transferError(err: Error, org: string): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Try again once it is running.';
  if (err.code === 'not-an-admin') return `Only an admin of ${org} can become its owner. Choose an admin.`;
  if (err.code === 'confirmation-mismatch') return `Type ${org} exactly as it is written.`;
  return err.detail ?? err.title;
}

/** The Transfer ownership row: the admins to choose from, and the Transfer button that opens the dialog. */
export function TransferOwnership({ org, name }: { org: OrgSummary; name: string }) {
  const members = useQuery(membersQuery(org.id));
  const admins = (members.data ?? []).filter((m) => m.role === 'admin');
  const [to, setTo] = useState('');
  const [open, setOpen] = useState(false);
  const chosen = to || admins[0]?.user.id || '';
  const chosenName = admins.find((m) => m.user.id === chosen)?.user;
  const none = members.isSuccess && admins.length === 0;

  return (
    <>
      <SettingsRow
        id="transfer-to"
        label="Transfer ownership"
        help={
          none ? (
            <>
              Make someone an admin first.{' '}
              <Link to="/o/$org/members" params={{ org: org.slug }} className="text-ink underline">
                Open Members and roles
              </Link>
            </>
          ) : (
            'You become an Admin'
          )
        }
      >
        <div className="flex flex-wrap items-center gap-2">
          <NativeSelect
            id="transfer-to"
            value={chosen}
            onChange={(e) => setTo(e.target.value)}
            disabled={!members.isSuccess || none}
            className="w-auto"
          >
            {admins.map((m) => (
              <option key={m.user.id} value={m.user.id}>
                {displayName(m.user)}
              </option>
            ))}
          </NativeSelect>
          <Button variant="raised" className="text-rework-ink" disabled={!chosen || none} onClick={() => setOpen(true)}>
            Transfer
          </Button>
        </div>
      </SettingsRow>
      {open ? (
        <TransferDialog
          org={org}
          name={name}
          to={chosenName ? displayName(chosenName) : ''}
          toId={chosen}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  );
}

function TransferDialog({
  org,
  name,
  to,
  toId,
  onClose,
}: {
  org: OrgSummary;
  name: string;
  /** The new owner's name, as the row shows it. */
  to: string;
  toId: string;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const router = useRouter();
  const [typed, setTyped] = useState('');
  const typedRef = useRef<HTMLInputElement>(null);

  const transfer = useMutation({
    mutationFn: () => transferOwnership(org.id, toId, typed),
    onSuccess: async () => {
      onClose();
      toast.success(`You’re now an admin of ${name}`);
      await Promise.all([
        client.invalidateQueries({ queryKey: keys.detail(org.id) }),
        client.invalidateQueries({ queryKey: keys.members(org.id) }),
        client.invalidateQueries({ queryKey: keys.orgs }),
      ]);
      // The sidebar and the menu read the role from the route's organization.
      await router.invalidate();
    },
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (typed === name && !transfer.isPending) transfer.mutate();
  };

  return (
    <Dialog open onOpenChange={(next) => (next ? null : onClose())}>
      <DialogContent
        title={`Transfer ownership of ${name}`}
        description={`The new owner can delete ${name} and transfer it again. You become an admin.`}
        initialFocus={typedRef}
      >
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
          <p className="m-0 text-sm text-ink-soft">
            New owner: <span className="font-semibold text-ink">{to}</span>
          </p>
          <div className="flex flex-col gap-2">
            <label htmlFor="transfer-confirm" className="text-sm font-medium text-ink">
              Type <strong className="font-semibold">{name}</strong> to confirm
            </label>
            <Input
              id="transfer-confirm"
              ref={typedRef}
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
            />
          </div>
          {transfer.error ? (
            <p role="alert" className="m-0 text-sm text-rework-ink">
              {transferError(transfer.error, name)}
            </p>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary" size="lg" className="md:h-10 md:text-sm">
                Cancel
              </Button>
            </DialogClose>
            <Button
              type="submit"
              variant="rework"
              size="lg"
              className="md:h-10 md:text-sm"
              disabled={typed !== name || transfer.isPending}
            >
              {transfer.isPending ? 'Transferring…' : 'Transfer ownership'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
