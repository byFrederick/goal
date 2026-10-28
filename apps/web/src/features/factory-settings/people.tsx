// People with access: the members who see this factory (owners and admins, and members with a factory role
// on it), each with what the role allows and its chip. Adding a person and changing or removing a member's
// role go through goald's factory-role endpoints (usePeopleChanges). Owners and admins hold every role, so
// their rows have no change control.
import { useQuery } from '@tanstack/react-query';
import { type FormEvent, useState } from 'react';
import { membersQuery } from '../../api/queries';
import type { Factory, FactoryRole, Member, OrgSummary } from '../../api/types';
import { factoryTitle } from '../../shared/factory';
import { displayName, initialsOf } from '../../shared/names';
import { useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { Field, NativeSelect } from '../../ui/field';
import { Avatar } from '../../ui/mark';
import { Tooltip } from '../../ui/tooltip';
import {
  byRole,
  factoryAccessLines,
  factoryRoleNames,
  factoryRoles,
  orgRoleNames,
  seesFactory,
} from '../members/people';
import { usePeopleChanges } from '../members/use-people';

/** The chip and the line under the name: a factory role, or the organization role for owners and admins. */
function roleOf(member: Member, factory: string): { chip: string; line: string } {
  if (member.role !== 'member') return { chip: orgRoleNames[member.role], line: factoryAccessLines[member.role] };
  const held = member.factory_roles.find((g) => g.factory_id === factory)?.role ?? 'viewer';
  return { chip: factoryRoleNames[held], line: factoryAccessLines[held] };
}

interface Props {
  org: OrgSummary;
  factory: Factory;
}

export function PeopleCard({ org, factory }: Props) {
  const members = useQuery(membersQuery(org.id));
  const mayManage = useOrgHolds('member:manage');
  const changes = usePeopleChanges(org);
  const [adding, setAdding] = useState(false);
  const [changing, setChanging] = useState<Member>();
  const listed = byRole((members.data ?? []).filter((m) => seesFactory(m, factory.id)));
  // Owners and admins hold every factory role already, so only members can be added to a role here.
  const candidates = (members.data ?? []).filter((m) => m.role === 'member' && !seesFactory(m, factory.id));

  return (
    <SectionCard id="people" title="People with access" aria-busy={members.isPending || undefined}>
      {members.isPending ? <span className="sr-only">Loading</span> : null}
      {members.isError ? (
        <p role="alert" className="m-0 text-secondary text-rework-ink">
          goald is not answering. Try again once it is running.
        </p>
      ) : null}
      {members.data && listed.length === 0 ? (
        <p className="m-0 text-secondary text-muted">No people with access yet.</p>
      ) : null}
      {listed.map((member) => {
        const { chip, line } = roleOf(member, factory.id);
        const changeable = mayManage && member.role === 'member';
        return (
          <div key={member.member_id} className="flex items-center gap-3 border-t border-line pt-3">
            <Avatar initials={initialsOf(member.user)} size="sidebar" />
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="font-control text-ink">{displayName(member.user)}</span>
              <span className="text-meta text-muted">{line}</span>
            </span>
            <Chip>{chip}</Chip>
            {changeable ? (
              <Button size="sm" variant="secondary" onClick={() => setChanging(member)}>
                Change role
              </Button>
            ) : null}
          </div>
        );
      })}
      <div className="border-t border-line pt-3">
        {mayManage ? (
          <Button size="sm" variant="secondary" onClick={() => setAdding(true)}>
            Add people
          </Button>
        ) : (
          <Tooltip label="Only owners and admins change who has access.">
            <Button size="sm" variant="secondary" disabled>
              Add people
            </Button>
          </Tooltip>
        )}
      </div>
      <AddPersonDialog
        open={adding}
        onOpenChange={setAdding}
        candidates={candidates}
        factory={factory}
        onAdd={(member, to) => changes.factoryRole.mutateAsync({ member, factory: factory.id, to })}
      />
      {changing ? (
        <RoleDialog
          key={changing.member_id}
          member={changing}
          onClose={() => setChanging(undefined)}
          factory={factory}
          onChange={(member, to) => changes.factoryRole.mutateAsync({ member, factory: factory.id, to })}
        />
      ) : null}
    </SectionCard>
  );
}

/** Add people: an organization member with no role here, and the role they get. Errors show in a toast. */
function AddPersonDialog({
  open,
  onOpenChange,
  candidates,
  factory,
  onAdd,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  candidates: Member[];
  factory: Factory;
  onAdd: (member: Member, to: FactoryRole) => Promise<unknown>;
}) {
  const [userId, setUserId] = useState('');
  const [role, setRole] = useState<FactoryRole>('viewer');
  const [pending, setPending] = useState(false);
  const chosen = candidates.find((m) => m.user.id === userId) ?? candidates[0];

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!chosen) return;
    setPending(true);
    try {
      await onAdd(chosen, role);
      onOpenChange(false);
    } catch {
      // The refusal is a toast (usePeopleChanges); the dialog stays open to try again.
    } finally {
      setPending(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title="Add people" description={`Give a role on ${factoryTitle(factory)}.`}>
        {chosen ? (
          <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4">
            <Field label="Person">
              <NativeSelect value={chosen.user.id} onChange={(e) => setUserId(e.target.value)}>
                {candidates.map((m) => (
                  <option key={m.user.id} value={m.user.id}>
                    {displayName(m.user)}
                  </option>
                ))}
              </NativeSelect>
            </Field>
            <Field label="Role">
              <NativeSelect value={role} onChange={(e) => setRole(e.target.value as FactoryRole)}>
                {factoryRoles.map((r) => (
                  <option key={r} value={r}>
                    {factoryRoleNames[r]}
                  </option>
                ))}
              </NativeSelect>
            </Field>
            <div className="flex flex-wrap justify-end gap-2">
              <DialogClose asChild>
                <Button variant="secondary">Cancel</Button>
              </DialogClose>
              <Button type="submit" variant="primary" disabled={pending}>
                {pending ? 'Saving…' : 'Add'}
              </Button>
            </div>
          </form>
        ) : (
          <p className="m-0 text-secondary text-muted">Everyone in the organization already has a role here.</p>
        )}
      </DialogContent>
    </Dialog>
  );
}

/** Change a member's role on the factory, or take it away (the member no longer sees the factory). */
function RoleDialog({
  member,
  onClose,
  factory,
  onChange,
}: {
  member: Member;
  onClose: () => void;
  factory: Factory;
  onChange: (member: Member, to: FactoryRole | null) => Promise<unknown>;
}) {
  const current = member.factory_roles.find((g) => g.factory_id === factory.id)?.role ?? 'viewer';
  const [role, setRole] = useState<FactoryRole>(current);
  const [pending, setPending] = useState(false);

  const run = async (to: FactoryRole | null) => {
    setPending(true);
    try {
      await onChange(member, to);
      onClose();
    } catch {
      // The refusal is a toast (usePeopleChanges); the dialog stays open to try again.
    } finally {
      setPending(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={`${displayName(member.user)} on ${factoryTitle(factory)}`}
        description="Changes apply at once."
      >
        <form
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            void run(role);
          }}
          className="flex flex-col gap-4"
        >
          <Field label="Role">
            <NativeSelect value={role} onChange={(e) => setRole(e.target.value as FactoryRole)}>
              {factoryRoles.map((r) => (
                <option key={r} value={r}>
                  {factoryRoleNames[r]}
                </option>
              ))}
            </NativeSelect>
          </Field>
          <div className="flex flex-wrap justify-between gap-2">
            <Button type="button" variant="secondary" disabled={pending} onClick={() => void run(null)}>
              Remove from factory
            </Button>
            <div className="flex flex-wrap gap-2">
              <DialogClose asChild>
                <Button variant="secondary">Cancel</Button>
              </DialogClose>
              <Button type="submit" variant="primary" disabled={pending}>
                {pending ? 'Saving…' : 'Save'}
              </Button>
            </div>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
