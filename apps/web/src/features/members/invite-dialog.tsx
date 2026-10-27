// Invite people (spec 3 §10.2; canvas "Members-Invite"): addresses one per line (up to 20), the
// role in the organization (Owner only for owners), and for members a role per factory, default
// None. Each address gets its own invitation from the identity service; one that is refused is
// named and the dialog stays open with only the addresses that did not go.
import type { UseMutationResult } from '@tanstack/react-query';
import { type FormEvent, useId, useRef, useState } from 'react';
import { toast } from 'sonner';
import type { InviteRequest } from '../../api/auth';
import type { Factory, FactoryRole, OrgRole, OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { Field, NativeSelect, Textarea } from '../../ui/field';
import { RadioList } from '../../ui/radio-cards';
import { factoryRoleNames, factoryRoles, maxInvites, parseEmails } from './people';
import { refusal } from './use-people';

type Refused = { email: string; err: unknown }[];

interface InviteDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  org: OrgSummary;
  /** The inviter's role: only owners invite owners. */
  role: OrgRole | null | undefined;
  factories: Factory[];
  invite: UseMutationResult<Refused, Error, InviteRequest[]>;
}

const roleOptions = (org: string, owner: boolean) => [
  { value: 'member' as const, label: 'Member', description: `Sees ${org}. The factory roles below decide the rest.` },
  { value: 'admin' as const, label: 'Admin', description: 'Manages people, keys and every factory.' },
  ...(owner
    ? [
        {
          value: 'owner' as const,
          label: 'Owner',
          description: `Everything, including transferring and deleting ${org}. Only owners can invite an owner.`,
        },
      ]
    : []),
];

export function InviteDialog({ open, onOpenChange, org, role, factories, invite }: InviteDialogProps) {
  const [text, setText] = useState('');
  const [inviteAs, setInviteAs] = useState<OrgRole>('member');
  const [grants, setGrants] = useState<Record<string, FactoryRole | ''>>({});
  const [error, setError] = useState<string | undefined>();
  const [refused, setRefused] = useState<string[]>([]);
  const emailsRef = useRef<HTMLTextAreaElement>(null);
  const ids = useId();

  const reset = () => {
    setText('');
    setInviteAs('member');
    setGrants({});
    setError(undefined);
    setRefused([]);
    invite.reset();
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (invite.isPending) return;
    const { emails, invalid } = parseEmails(text);
    const problem =
      invalid.length > 0
        ? `Not an email address: ${invalid.join(', ')}.`
        : emails.length === 0
          ? 'Enter at least one email address.'
          : emails.length > maxInvites
            ? `At most ${maxInvites} addresses at once.`
            : undefined;
    setError(problem);
    if (problem) {
      emailsRef.current?.focus();
      return;
    }
    const factoryRolesOf = Object.entries(grants).flatMap(([factoryId, r]) => (r ? [{ factoryId, role: r }] : []));
    const requests: InviteRequest[] = emails.map((email) => ({
      organizationId: org.id,
      email,
      role: inviteAs,
      factoryRoles: inviteAs === 'member' ? factoryRolesOf : [],
    }));
    invite.mutate(requests, {
      onSuccess: (failed) => {
        const sent = emails.length - failed.length;
        if (failed.length === 0) {
          toast.success(sent === 1 ? `Invitation sent to ${emails[0]}` : `${sent} invitations sent`);
          onOpenChange(false);
          reset();
          return;
        }
        // Keep the addresses that did not go, and say why each did not.
        setText(failed.map((f) => f.email).join('\n'));
        setRefused(failed.map((f) => `${f.email}: ${refusal(f.err, org.name)}`));
        if (sent > 0) toast.success(sent === 1 ? '1 invitation sent' : `${sent} invitations sent`);
      },
    });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset();
        onOpenChange(next);
      }}
    >
      <DialogContent title={`Invite people to ${org.name}`} initialFocus={emailsRef} className="max-w-[540px]">
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-5">
          <Field
            label="Email addresses"
            help="One per line, up to 20. Each person gets an email with a link that expires in 3 days."
            error={error}
          >
            <Textarea
              ref={emailsRef}
              rows={3}
              value={text}
              onChange={(e) => setText(e.target.value)}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              placeholder="d.ross@example.com"
            />
          </Field>
          <RadioList
            legend={`Role in ${org.name}`}
            showLegend
            name={`${ids}-role`}
            options={roleOptions(org.name, role === 'owner')}
            value={inviteAs}
            onChange={setInviteAs}
          />
          {inviteAs === 'member' && factories.length > 0 ? (
            <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
              <legend className="mb-2 p-0 text-body font-medium">Factory roles</legend>
              {[...factories]
                .sort((a, b) => a.name.localeCompare(b.name))
                .map((f) => (
                  <Field
                    key={f.id}
                    label={f.name}
                    className="grid grid-cols-[minmax(0,1fr)_160px] items-center gap-x-4 [&>label]:font-normal"
                  >
                    <NativeSelect
                      value={grants[f.id] ?? ''}
                      onChange={(e) => setGrants((g) => ({ ...g, [f.id]: e.target.value as FactoryRole | '' }))}
                    >
                      <option value="">None</option>
                      {factoryRoles.map((r) => (
                        <option key={r} value={r}>
                          {factoryRoleNames[r]}
                        </option>
                      ))}
                    </NativeSelect>
                  </Field>
                ))}
              <p className="m-0 text-secondary leading-snug text-muted">
                Applied when they join. Owners and admins are editors in every factory, so these apply to members only.
              </p>
            </fieldset>
          ) : null}
          <div aria-live="polite">
            {refused.length > 0 ? (
              <ul className="m-0 flex list-none flex-col gap-1 rounded-control border border-rework-tint bg-rework-tint px-3 py-2 text-body text-rework-ink">
                {refused.map((r) => (
                  <li key={r}>Not sent to {r}</li>
                ))}
              </ul>
            ) : null}
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="white" size="lg" className="md:h-10 md:text-sm">
                Cancel
              </Button>
            </DialogClose>
            <Button
              type="submit"
              variant="primary"
              size="lg"
              className="md:h-10 md:text-sm"
              disabled={invite.isPending}
            >
              {invite.isPending ? 'Sending…' : 'Send invitations'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
