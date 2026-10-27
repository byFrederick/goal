// Members and roles (spec 3 §10.2; canvas "Members", "Members-View", "Mobile-Members"): everyone
// of the organization with their organization role, their factory roles and when they were last
// active, the pending invitations, each person's role in each factory, and what each role can do.
// Owners and admins invite, change roles, remove and give factory roles; a control they cannot use
// says why ("Only owners change an owner"). Members see the same page read-only and may leave.
// From 720 px wide a table; below, a list of person cards whose details open in a sheet.
import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { ChevronRight, Ellipsis, Plus, Search, X } from 'lucide-react';
import { type ReactNode, type RefObject, useId, useRef, useState } from 'react';
import { authConfigQuery, factoriesQuery, invitationsQuery, membersQuery, orgQuery } from '../../api/queries';
import type { Factory, FactoryRole, Invitation, Member, Org, OrgRole, OrgSummary } from '../../api/types';
import { displayName, hasName, initialsOf } from '../../shared/names';
import { useOrg } from '../../shared/org';
import { reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { Field, NativeSelect } from '../../ui/field';
import { Avatar } from '../../ui/mark';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../ui/menu';
import { descriptionClass, PageHeader } from '../../ui/page-header';
import { SegmentedControl, type SegmentedOption } from '../../ui/segmented-control';
import { Sheet, SheetContent } from '../../ui/sheet';
import { ViewOnlyBanner } from '../../ui/view-only-banner';
import { InviteDialog } from './invite-dialog';
import {
  byRole,
  countTitle,
  editorEverywhere,
  factoryRoleNames,
  factoryRoles,
  factorySummary,
  invitationLine,
  lastActive,
  matches,
  orgRoleNames,
  type RoleChange,
  roleCapabilities,
  roleChange,
} from './people';
import { refusal as refusalText, usePeopleChanges } from './use-people';

/** What the caller may do on this page. */
interface Powers {
  /** Invite, change organization roles, remove: owners and admins, with sign-in on. */
  manages: boolean;
  /** Give and take factory roles (goald's): owners and admins. */
  grants: boolean;
  role: OrgRole | null | undefined;
}

export function MembersPage() {
  const org = useOrg();
  const { data: detail } = useSuspenseQuery(orgQuery(org.id));
  const { data: members } = useSuspenseQuery(membersQuery(org.id));
  const { data: factories } = useSuspenseQuery(factoriesQuery(org.id));
  const { data: config } = useSuspenseQuery(authConfigQuery);
  const readsInvitations = detail.can.includes('invitation:read');
  const { data: invitations = [] } = useQuery({ ...invitationsQuery(org.id), enabled: readsInvitations });
  const powers: Powers = {
    manages: config.mode === 'session' && detail.can.includes('member:manage'),
    grants: detail.can.includes('member:manage'),
    role: detail.role,
  };
  return (
    <Members
      org={org}
      detail={detail}
      members={members}
      invitations={invitations}
      factories={factories}
      powers={powers}
      signIn={config.mode === 'session'}
    />
  );
}

interface MembersProps {
  org: OrgSummary;
  detail: Org;
  members: Member[];
  invitations: Invitation[];
  factories: Factory[];
  powers: Powers;
  signIn: boolean;
}

type Asking = { kind: 'remove' | 'leave'; member: Member } | null;

function Members({ org, detail, members, invitations, factories, powers, signIn }: MembersProps) {
  const changes = usePeopleChanges(org);
  const [query, setQuery] = useState('');
  const [inviting, setInviting] = useState(false);
  const [asking, setAsking] = useState<Asking>(null);
  const [sheetFor, setSheetFor] = useState<string | null>(null);
  // The person the factory panel shows: chosen from a row's menu or the panel's own select.
  const [chosen, setChosen] = useState<string | null>(null);
  const panelRef = useRef<HTMLSelectElement>(null);

  const owners = members.filter((m) => m.role === 'owner').length;
  const shown = byRole(members).filter((m) => matches(m, query));
  const shownInvitations = invitations.filter((i) => i.email.includes(query.trim().toLowerCase()));
  const focus = members.find((m) => m.user.id === chosen) ?? shown.find((m) => m.role === 'member') ?? shown[0];
  const sheetMember = members.find((m) => m.user.id === sheetFor);
  const change = (m: Member) => roleChange(m, powers, owners, org.name);

  const showFactoryRoles = (m: Member) => {
    setChosen(m.user.id);
    // After the menu closes and returns focus to its trigger.
    requestAnimationFrame(() => {
      panelRef.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
      panelRef.current?.focus();
    });
  };

  const actions: RowActions = {
    onFactoryRoles: showFactoryRoles,
    onRemove: (m) => setAsking({ kind: 'remove', member: m }),
    onLeave: (m) => setAsking({ kind: 'leave', member: m }),
    onRole: (m, to) => changes.changeRole.mutate({ member: m, to }),
    rolePending: changes.changeRole.isPending ? changes.changeRole.variables?.member.user.id : undefined,
  };

  return (
    <>
      <PageHeader
        title={
          <>
            <span className="md:hidden">Members</span>
            <span className="hidden md:inline">Members and roles</span>
          </>
        }
        description={
          <p className={cn(descriptionClass, 'hidden md:block')}>
            {powers.manages
              ? `Who belongs to ${org.name}, what they can change in the organization, and what they can do in each factory.`
              : `Who belongs to ${org.name} and what each person can do in the organization and in each factory.`}
          </p>
        }
        actions={
          powers.manages ? (
            <Button variant="primary" className="h-11 md:h-10" onClick={() => setInviting(true)}>
              <Plus size={16} strokeWidth={1.75} aria-hidden="true" />
              <span className="md:hidden">Invite</span>
              <span className="hidden md:inline">Invite people</span>
            </Button>
          ) : null
        }
      />

      {powers.manages ? null : (
        <ViewOnlyBanner>
          {signIn
            ? reasons.members(org.name)
            : 'Sign-in is off on this install: people join and change roles once it is on.'}
        </ViewOnlyBanner>
      )}

      <section
        aria-labelledby="people"
        className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-3 md:p-5"
      >
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 id="people" className="m-0 text-base font-medium md:text-lg">
            {countTitle(members.length, invitations.length)}
          </h2>
          <SearchField value={query} onChange={setQuery} />
        </div>

        {/* From 720 px: a table. */}
        <table className="hidden w-full border-collapse text-left @[720px]:table">
          <thead>
            <tr className="text-[13px] text-muted [&>th]:border-b [&>th]:border-line [&>th]:px-1 [&>th]:pb-2 [&>th]:font-normal">
              <th scope="col">Person</th>
              <th scope="col">In {org.name}</th>
              <th scope="col">In factories</th>
              <th scope="col">Last active</th>
              <th scope="col">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {shown.map((m) => (
              <MemberRow
                key={m.member_id}
                member={m}
                org={org}
                factories={factories}
                change={change(m)}
                powers={powers}
                signIn={signIn}
                actions={actions}
              />
            ))}
            {shownInvitations.map((i) => (
              <InvitationRow key={i.id} invitation={i} factories={factories} powers={powers} changes={changes} />
            ))}
          </tbody>
        </table>

        {/* Below 720 px: person cards; each opens its details in a sheet. */}
        <ul className="m-0 flex list-none flex-col gap-2 p-0 @[720px]:hidden">
          {shown.map((m) => (
            <li key={m.member_id}>
              <button
                type="button"
                onClick={() => setSheetFor(m.user.id)}
                className="grid min-h-16 w-full cursor-pointer grid-cols-[40px_minmax(0,1fr)_20px] items-center gap-3 rounded-control border border-line bg-card px-3 py-2.5 text-left text-ink transition-colors duration-200 hover:border-rule"
              >
                <Avatar initials={initialsOf(m.user)} size="card" />
                <span className="min-w-0">
                  <span className="flex items-center gap-2 text-[15px] font-medium">
                    <span className="truncate">{displayName(m.user)}</span>
                    {m.you ? <Chip>You</Chip> : null}
                  </span>
                  <span className="block text-[13px] text-muted">
                    {orgRoleNames[m.role]}. {factorySummary(m, factories)}
                  </span>
                </span>
                <ChevronRight size={18} strokeWidth={1.5} aria-hidden="true" className="text-muted" />
              </button>
            </li>
          ))}
          {shownInvitations.map((i) => (
            <li key={i.id}>
              <InvitationCard invitation={i} factories={factories} powers={powers} changes={changes} />
            </li>
          ))}
        </ul>
        {shown.length === 0 && shownInvitations.length === 0 ? (
          <p className="m-0 py-3 text-sm text-muted">Nobody matches “{query.trim()}”.</p>
        ) : null}
      </section>

      <div className="grid gap-5 @[960px]:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)] @[960px]:items-start">
        {focus ? (
          <div className="hidden @[720px]:block">
            <FactoryPanel
              org={org}
              members={members}
              member={focus}
              factories={factories}
              powers={powers}
              onChoose={setChosen}
              selectRef={panelRef}
              onChange={(factory, to) => changes.factoryRole.mutate({ member: focus, factory, to })}
            />
          </div>
        ) : null}
        <RolesCard org={detail.name} />
      </div>

      {sheetMember ? (
        <Sheet open onOpenChange={(open) => (open ? null : setSheetFor(null))}>
          <MemberSheet
            member={sheetMember}
            org={org}
            factories={factories}
            change={change(sheetMember)}
            powers={powers}
            signIn={signIn}
            actions={actions}
            onFactoryRole={(factory, to) => changes.factoryRole.mutate({ member: sheetMember, factory, to })}
          />
        </Sheet>
      ) : null}

      {powers.manages ? (
        <InviteDialog
          open={inviting}
          onOpenChange={setInviting}
          org={org}
          role={powers.role}
          factories={factories}
          invite={changes.invite}
        />
      ) : null}

      <ConfirmDialog
        open={asking?.kind === 'remove'}
        onOpenChange={(open) => (open ? null : setAsking(null))}
        title={`Remove ${asking ? displayName(asking.member.user) : ''} from ${org.name}?`}
        description={`They lose access to ${org.name} and its factories at once, and their factory roles go too.`}
        confirmLabel="Remove"
        pendingLabel="Removing…"
        tone="danger"
        pending={changes.remove.isPending}
        onConfirm={() => {
          if (asking) changes.remove.mutate(asking.member, { onSuccess: () => setAsking(null) });
        }}
      >
        <MutationError error={changes.remove.error} org={org.name} />
      </ConfirmDialog>
      <ConfirmDialog
        open={asking?.kind === 'leave'}
        onOpenChange={(open) => (open ? null : setAsking(null))}
        title={`Leave ${org.name}?`}
        description={`You lose access to ${org.name} and its factories at once. An owner can invite you again.`}
        confirmLabel={`Leave ${org.name}`}
        pendingLabel="Leaving…"
        tone="danger"
        pending={changes.leave.isPending}
        onConfirm={() => changes.leave.mutate()}
      >
        <MutationError error={changes.leave.error} org={org.name} />
      </ConfirmDialog>
    </>
  );
}

function MutationError({ error, org }: { error: unknown; org: string }) {
  return error ? (
    <p role="alert" className="m-0 text-sm text-rework-ink">
      {refusalText(error, org)}
    </p>
  ) : null;
}

function SearchField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const id = useId();
  return (
    <div className="relative w-full @[720px]:w-72">
      <label htmlFor={id} className="sr-only">
        Find a person
      </label>
      <Search
        size={16}
        strokeWidth={1.5}
        aria-hidden="true"
        className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
      />
      <input
        id={id}
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="Find a person"
        autoComplete="off"
        className="h-11 w-full rounded-control border border-rule bg-card pr-3 pl-9 text-base text-ink transition-colors duration-200 hover:border-muted md:h-10 md:text-[15px]"
      />
    </div>
  );
}

interface RowActions {
  onFactoryRoles: (m: Member) => void;
  onRemove: (m: Member) => void;
  onLeave: (m: Member) => void;
  onRole: (m: Member, to: OrgRole) => void;
  /** The person whose organization role is being changed now. */
  rolePending: string | undefined;
}

function Person({ member }: { member: Member }) {
  return (
    <span className="flex min-w-0 items-center gap-3">
      <Avatar initials={initialsOf(member.user)} size="card" />
      <span className="min-w-0">
        <span className="flex items-center gap-2 text-[15px] font-medium">
          <span className="truncate">{displayName(member.user)}</span>
          {member.you ? <Chip>You</Chip> : null}
        </span>
        {hasName(member.user) ? (
          <span className="block truncate text-[13px] text-muted">{member.user.email}</span>
        ) : null}
      </span>
    </span>
  );
}

/** The organization role: a select where it can change, else the role, with why when it cannot. */
function RoleControl({
  member,
  org,
  change,
  powers,
  actions,
  className,
}: {
  member: Member;
  org: OrgSummary;
  change: RoleChange;
  powers: Powers;
  actions: RowActions;
  className?: string;
}) {
  const reasonId = useId();
  const label = `Role of ${displayName(member.user)} in ${org.name}`;
  if (!powers.manages) return <span className="text-[15px]">{orgRoleNames[member.role]}</span>;
  // Only an owner makes an owner.
  const choices: OrgRole[] =
    powers.role === 'owner' || member.role === 'owner' ? ['owner', 'admin', 'member'] : ['admin', 'member'];
  return (
    <span className={cn('flex flex-col gap-1', className)}>
      <NativeSelect
        aria-label={label}
        aria-describedby={change.reason ? reasonId : undefined}
        value={member.role}
        disabled={!change.editable || actions.rolePending === member.user.id}
        onChange={(e) => actions.onRole(member, e.target.value as OrgRole)}
        className="w-auto min-w-28 disabled:cursor-not-allowed disabled:opacity-60"
      >
        {choices.map((r) => (
          <option key={r} value={r}>
            {orgRoleNames[r]}
          </option>
        ))}
      </NativeSelect>
      {change.reason ? (
        <span id={reasonId} className="max-w-40 text-xs leading-snug text-muted">
          {change.reason}
        </span>
      ) : null}
    </span>
  );
}

function MemberRow({
  member,
  org,
  factories,
  change,
  powers,
  signIn,
  actions,
}: {
  member: Member;
  org: OrgSummary;
  factories: Factory[];
  change: RoleChange;
  powers: Powers;
  signIn: boolean;
  actions: RowActions;
}) {
  return (
    <tr className="[&>td]:border-b [&>td]:border-line [&>td]:px-1 [&>td]:py-3 [&>td]:align-middle">
      <th scope="row" className="border-b border-line px-1 py-3 text-left font-normal">
        <Person member={member} />
      </th>
      <td>
        <RoleControl member={member} org={org} change={change} powers={powers} actions={actions} />
      </td>
      <td className="text-[15px]">{factorySummary(member, factories)}</td>
      <td className="text-[15px] text-muted">{lastActive(member.last_active_at)}</td>
      <td className="text-right">
        <RowMenu member={member} org={org} powers={powers} signIn={signIn} actions={actions} />
      </td>
    </tr>
  );
}

/** A row's actions: Factory roles…, Remove from {org}…, and on one's own row Leave {org}…. */
function RowMenu({
  member,
  org,
  powers,
  signIn,
  actions,
}: {
  member: Member;
  org: OrgSummary;
  powers: Powers;
  signIn: boolean;
  actions: RowActions;
}) {
  const leaves = signIn && member.you;
  if (!powers.manages) {
    return leaves ? (
      <Button variant="white" onClick={() => actions.onLeave(member)}>
        Leave {org.name}…
      </Button>
    ) : null;
  }
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="white" size="icon" aria-label={`Actions for ${displayName(member.user)}`} className="ml-auto">
          <Ellipsis size={18} strokeWidth={1.75} aria-hidden="true" />
        </Button>
      </MenuTrigger>
      <MenuContent side="bottom" align="end" className="w-64">
        <MenuItem onSelect={() => actions.onFactoryRoles(member)}>Factory roles…</MenuItem>
        {member.can_remove ? (
          <MenuItem onSelect={() => actions.onRemove(member)} className="text-rework-ink">
            <X size={16} strokeWidth={1.75} aria-hidden="true" />
            Remove from {org.name}…
          </MenuItem>
        ) : null}
        {leaves ? <MenuItem onSelect={() => actions.onLeave(member)}>Leave {org.name}…</MenuItem> : null}
      </MenuContent>
    </Menu>
  );
}

type Changes = ReturnType<typeof usePeopleChanges>;

/** The factory roles an invitation gives, in a line: "sre viewer". */
function invitationFactories(i: Invitation, factories: Factory[]): string {
  if (editorEverywhere(i.role)) return 'Editor in every factory';
  const names = new Map(factories.map((f) => [f.id, f.name]));
  const held = i.factory_roles.flatMap((g) => {
    const name = names.get(g.factory_id);
    return name ? [`${name} ${g.role}`] : [];
  });
  return held.length > 0 ? held.sort().join(', ') : 'No factories';
}

function InvitationActions({
  invitation,
  powers,
  changes,
}: {
  invitation: Invitation;
  powers: Powers;
  changes: Changes;
}) {
  if (!powers.manages) return null;
  const busy = (m: Changes['resend'] | Changes['cancel']) => m.isPending && m.variables?.id === invitation.id;
  return (
    <span className="flex justify-end gap-2">
      <Button
        variant="white"
        aria-label={`Resend the invitation to ${invitation.email}`}
        disabled={busy(changes.resend)}
        onClick={() => changes.resend.mutate(invitation)}
      >
        Resend
      </Button>
      <Button
        variant="secondary"
        aria-label={`Cancel the invitation to ${invitation.email}`}
        disabled={busy(changes.cancel)}
        onClick={() => changes.cancel.mutate(invitation)}
      >
        Cancel
      </Button>
    </span>
  );
}

function PendingAvatar() {
  return (
    <span
      aria-hidden="true"
      className="flex size-10 shrink-0 items-center justify-center rounded-full border border-dashed border-rule bg-card text-sm text-muted"
    >
      ?
    </span>
  );
}

function InvitationRow({
  invitation,
  factories,
  powers,
  changes,
}: {
  invitation: Invitation;
  factories: Factory[];
  powers: Powers;
  changes: Changes;
}) {
  return (
    <tr className="[&>td]:border-b [&>td]:border-line [&>td]:px-1 [&>td]:py-3 [&>td]:align-middle">
      <th scope="row" className="border-b border-line px-1 py-3 text-left font-normal">
        <span className="flex min-w-0 items-center gap-3">
          <PendingAvatar />
          <span className="min-w-0">
            <span className="block truncate text-[15px] font-medium">{invitation.email}</span>
            <span className="block text-[13px] text-muted">{invitationLine(invitation)}</span>
          </span>
        </span>
      </th>
      <td className="text-[15px]">{orgRoleNames[invitation.role]}</td>
      <td className="text-[15px]">{invitationFactories(invitation, factories)}</td>
      <td className="text-[15px] text-muted">Invite pending</td>
      <td>
        <InvitationActions invitation={invitation} powers={powers} changes={changes} />
      </td>
    </tr>
  );
}

function InvitationCard({
  invitation,
  factories,
  powers,
  changes,
}: {
  invitation: Invitation;
  factories: Factory[];
  powers: Powers;
  changes: Changes;
}) {
  return (
    <div className="flex flex-col gap-2.5 rounded-control border border-dashed border-rule bg-card px-3 py-2.5">
      <span className="grid grid-cols-[40px_minmax(0,1fr)] items-center gap-3">
        <PendingAvatar />
        <span className="min-w-0">
          <span className="block truncate text-[15px] font-medium">{invitation.email}</span>
          <span className="block text-[13px] text-muted">
            Invite pending. {orgRoleNames[invitation.role]}. {invitationFactories(invitation, factories)}
          </span>
          <span className="block text-[13px] text-muted">{invitationLine(invitation)}</span>
        </span>
      </span>
      <InvitationActions invitation={invitation} powers={powers} changes={changes} />
    </div>
  );
}

type FactoryChoice = FactoryRole | 'none';

/** One person's role on each factory: a choice of None, Viewer, Approver, Editor per factory. */
function FactoryRoles({
  member,
  factories,
  powers,
  onChange,
  stacked = false,
}: {
  member: Member;
  factories: Factory[];
  powers: Powers;
  onChange: (factory: string, to: FactoryRole | null) => void;
  stacked?: boolean;
}) {
  const everywhere = editorEverywhere(member.role);
  const reason = everywhere
    ? 'Editor in every factory, from their organization role'
    : powers.grants
      ? undefined
      : 'Owners and admins change factory roles';
  const options: SegmentedOption<FactoryChoice>[] = [
    { value: 'none', label: 'None', disabledReason: reason },
    ...factoryRoles.map((r) => ({ value: r, label: factoryRoleNames[r], disabledReason: reason })),
  ];
  if (factories.length === 0) return <p className="m-0 text-sm text-muted">No factories yet.</p>;
  return (
    <ul className="m-0 flex list-none flex-col gap-2.5 p-0">
      {[...factories]
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((f) => {
          const held = member.factory_roles.find((g) => g.factory_id === f.id)?.role;
          const value: FactoryChoice = everywhere ? 'editor' : (held ?? 'none');
          return (
            <li
              key={f.id}
              className={cn('flex gap-2', stacked ? 'flex-col' : 'flex-wrap items-center justify-between gap-x-4')}
            >
              <span className="text-[15px]">{f.name}</span>
              <SegmentedControl
                label={`${f.name}: role of ${displayName(member.user)}`}
                options={options}
                value={value}
                onChange={(to) => onChange(f.id, to === 'none' ? null : to)}
                className={stacked ? 'grid w-full grid-cols-4 [&>*]:justify-center' : ''}
              />
            </li>
          );
        })}
    </ul>
  );
}

function FactoryPanel({
  org,
  members,
  member,
  factories,
  powers,
  onChoose,
  selectRef,
  onChange,
}: {
  org: OrgSummary;
  members: Member[];
  member: Member;
  factories: Factory[];
  powers: Powers;
  onChoose: (user: string) => void;
  selectRef: RefObject<HTMLSelectElement | null>;
  onChange: (factory: string, to: FactoryRole | null) => void;
}) {
  const everywhere = editorEverywhere(member.role);
  return (
    <SectionCard
      title={`${displayName(member.user)} in each factory`}
      description={
        everywhere
          ? `Editor in every factory (from their organization role). ${displayName(member.user)} is ${member.role === 'owner' ? 'an owner' : 'an admin'} of ${org.name}, so factory roles don’t apply.`
          : powers.grants
            ? 'Factory roles add to the organization role. Changes apply at once and go into the audit log.'
            : 'Factory roles add to the organization role.'
      }
    >
      <Field label="Person">
        <NativeSelect ref={selectRef} value={member.user.id} onChange={(e) => onChoose(e.target.value)}>
          {members.map((m) => (
            <option key={m.user.id} value={m.user.id}>
              {displayName(m.user)}
            </option>
          ))}
        </NativeSelect>
      </Field>
      <FactoryRoles member={member} factories={factories} powers={powers} onChange={onChange} />
    </SectionCard>
  );
}

function RolesCard({ org }: { org: string }) {
  return (
    <SectionCard title="What each role can do">
      <dl className="m-0 grid grid-cols-[96px_minmax(0,1fr)] gap-x-4 gap-y-3 text-[15px]">
        {roleCapabilities(org).map(({ role, can }) => (
          <Row key={role} term={role}>
            {can}
          </Row>
        ))}
      </dl>
    </SectionCard>
  );
}

function Row({ term, children }: { term: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-ink">{term}</dt>
      <dd className="m-0 leading-snug text-ink-soft">{children}</dd>
    </>
  );
}

/** A person's details on a phone: their organization role, factory roles, remove or leave. */
function MemberSheet({
  member,
  org,
  factories,
  change,
  powers,
  signIn,
  actions,
  onFactoryRole,
}: {
  member: Member;
  org: OrgSummary;
  factories: Factory[];
  change: RoleChange;
  powers: Powers;
  signIn: boolean;
  actions: RowActions;
  onFactoryRole: (factory: string, to: FactoryRole | null) => void;
}) {
  return (
    <SheetContent title={displayName(member.user)}>
      <div className="flex flex-col gap-4 px-2.5 pb-2">
        <p className="m-0 flex items-center gap-3 text-sm text-muted">
          <Avatar initials={initialsOf(member.user)} size="card" />
          <span className="min-w-0 break-words">
            {member.user.email},{' '}
            {member.last_active_at ? `active ${lastActive(member.last_active_at).toLowerCase()}` : 'never active'}
          </span>
        </p>
        <div className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">In {org.name}</span>
          <RoleControl
            member={member}
            org={org}
            change={change}
            powers={powers}
            actions={actions}
            className="[&>select]:w-full"
          />
        </div>
        <div className="flex flex-col gap-2">
          <span className="text-sm font-medium">In factories</span>
          <FactoryRoles member={member} factories={factories} powers={powers} onChange={onFactoryRole} stacked />
          {powers.grants && !editorEverywhere(member.role) ? (
            <p className="m-0 text-[13px] text-muted">Changes apply at once and go into the audit log.</p>
          ) : null}
        </div>
        {powers.manages && member.can_remove ? (
          <Button
            variant="white"
            size="lg"
            className="w-full justify-center text-rework-ink"
            onClick={() => actions.onRemove(member)}
          >
            Remove from {org.name}…
          </Button>
        ) : null}
        {signIn && member.you ? (
          <Button variant="white" size="lg" className="w-full justify-center" onClick={() => actions.onLeave(member)}>
            Leave {org.name}…
          </Button>
        ) : null}
      </div>
    </SheetContent>
  );
}
