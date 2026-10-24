// Transfer ownership (spec 3 §10.2; canvas "Org-Settings-Transfer"): an owner hands the
// organization to one of its admins, typing its name; in one change the admin becomes an owner and
// the owner an admin. Without an admin there is nobody to hand it to: the dialog says so and links
// to Members and roles. Needs a sign-in from the last 10 minutes (the re-auth dialog asks).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useRouter } from '@tanstack/react-router';
import { Info } from 'lucide-react';
import { type FormEvent, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, transferOwnership } from '../../api/client';
import { keys, membersQuery } from '../../api/queries';
import type { OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { Field, Input, NativeSelect } from '../../ui/field';

/** Why a transfer failed, in the dialog's words. */
function transferError(err: Error, org: string): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Try again once it is running.';
  if (err.code === 'not-an-admin') return `Only an admin of ${org} can become its owner. Choose an admin.`;
  if (err.code === 'confirmation-mismatch') return `Type ${org} exactly as it is written.`;
  return err.detail ?? err.title;
}

export function TransferOwnership({ org, name }: { org: OrgSummary; name: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-card border border-line bg-card p-4">
      <p className="m-0 flex min-w-0 flex-col">
        <span className="text-[15px] font-medium">Transfer ownership</span>
        <span className="text-sm text-muted">Hand {name} to one of its admins. You become an admin.</span>
      </p>
      <Button variant="raised" className="h-11 border border-line shadow-none md:h-10" onClick={() => setOpen(true)}>
        Transfer ownership…
      </Button>
      {open ? <TransferDialog org={org} name={name} onClose={() => setOpen(false)} /> : null}
    </div>
  );
}

function TransferDialog({ org, name, onClose }: { org: OrgSummary; name: string; onClose: () => void }) {
  const client = useQueryClient();
  const router = useRouter();
  const members = useQuery(membersQuery(org.id));
  const admins = (members.data ?? []).filter((m) => m.role === 'admin');
  const [to, setTo] = useState('');
  const [typed, setTyped] = useState('');
  const chosen = to || admins[0]?.user.id || '';
  const ownerRef = useRef<HTMLSelectElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);

  const transfer = useMutation({
    mutationFn: () => transferOwnership(org.id, chosen, typed),
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
    if (chosen && typed === name && !transfer.isPending) transfer.mutate();
  };

  const none = members.isSuccess && admins.length === 0;

  return (
    <Dialog open onOpenChange={(next) => (next ? null : onClose())}>
      <DialogContent
        title={`Transfer ownership of ${name}`}
        description={
          none
            ? `Only an admin can become the owner, and ${name} has none.`
            : `The new owner can delete ${name} and transfer it again. You become an admin.`
        }
        initialFocus={none ? closeRef : ownerRef}
      >
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
          {none ? (
            <p className="m-0 flex items-start gap-2.5 text-sm text-ink-soft">
              <Info size={16} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0 text-muted" />
              <span>
                Make someone an admin first.{' '}
                <Link to="/o/$org/members" params={{ org: org.slug }} className="text-ink underline">
                  Open Members and roles
                </Link>
              </span>
            </p>
          ) : (
            <>
              <Field label="New owner" help={`Only admins of ${name} can become its owner.`}>
                <NativeSelect
                  ref={ownerRef}
                  value={chosen}
                  onChange={(e) => setTo(e.target.value)}
                  disabled={!members.isSuccess}
                >
                  {admins.map((m) => (
                    <option key={m.user.id} value={m.user.id}>
                      {m.user.name}, admin
                    </option>
                  ))}
                </NativeSelect>
              </Field>
              <Field
                label={
                  <>
                    Type <strong className="font-semibold">{name}</strong> to confirm
                  </>
                }
              >
                <Input
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  autoComplete="off"
                  autoCapitalize="off"
                  spellCheck={false}
                />
              </Field>
            </>
          )}
          {transfer.error ? (
            <p role="alert" className="m-0 text-sm text-rework-ink">
              {transferError(transfer.error, name)}
            </p>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button ref={closeRef} variant="secondary" size="lg" className="md:h-10 md:text-sm">
                {none ? 'Close' : 'Cancel'}
              </Button>
            </DialogClose>
            <Button
              type="submit"
              variant="rework"
              size="lg"
              className="md:h-10 md:text-sm"
              disabled={none || !chosen || typed !== name || transfer.isPending}
            >
              {transfer.isPending ? 'Transferring…' : 'Transfer ownership'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
