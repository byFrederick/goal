// Members and roles (spec 3 §10.2; screen organization/members): everyone of the organization
// with their organization role, their factory roles and when they were last active, the people
// waiting to join, the invite link, and what each role can do. Owners and admins invite, change
// roles, remove and give factory roles; a control they cannot use says why ("Only owners change an
// owner"). Members see the same page read-only and may leave. Two columns from 960 px of content;
// below 720 px, person cards whose details open in a sheet.
import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { Check, ChevronDown, ChevronRight, Ellipsis, Minus, Plus, Search, X } from 'lucide-react';
import { Component, type ReactNode, Suspense, useId, useState } from 'react';
import { authConfigQuery, factoriesQuery, invitationsQuery, membersQuery, orgQuery } from '../../api/queries';
import type { Factory, FactoryRole, Invitation, Member, OrgRole, OrgSummary } from '../../api/types';
import { displayName, hasName, initialsOf } from '../../shared/names';
import { useOrg } from '../../shared/org';
import { reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { NativeSelect } from '../../ui/field';
import { Avatar } from '../../ui/mark';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../ui/menu';
import { NotAvailable } from '../../ui/not-available';
import { descriptionClass, PageHeader } from '../../ui/page-header';
import { SegmentedControl, type SegmentedOption } from '../../ui/segmented-control';
import { Sheet, SheetContent } from '../../ui/sheet';
import { Switch } from '../../ui/switch';
import { ViewOnlyBanner } from '../../ui/view-only-banner';
import { InviteDialog } from './invite-dialog';
import {
  byRole,
  countTitle,
  editorEverywhere,
  factoryRoleNames,
  factoryRoles,
  factorySummary,
  lastActive,
  matches,
  orgRoleNames,
  type RoleChange,
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

/** The loading state: the page background and a label for screen readers, no placeholder shapes. */
function MembersLoading() {
  return (
    <div aria-busy="true" className="min-h-40">
      <span className="sr-only">Loading</span>
    </div>
  );
}

/** A failed load says so in place of the page; the rest of the shell stays usable. */
class MembersFailed extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (this.state.failed) {
      return (
        <p role="alert" className="m-0 text-body text-rework-ink">
          Members could not load. Reload the page to try again.
        </p>
      );
    }
    return this.props.children;
  }
}

export function MembersPage() {
  return (
    <MembersFailed>
      <Suspense fallback={<MembersLoading />}>
        <MembersData />
      </Suspense>
    </MembersFailed>
  );
}

function MembersData() {
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
  members: Member[];
  invitations: Invitation[];
  factories: Factory[];
  powers: Powers;
  signIn: boolean;
}

type Asking = { kind: 'remove' | 'leave'; member: Member } | null;

function Members({ org, members, invitations, factories, powers, signIn }: MembersProps) {
  const changes = usePeopleChanges(org);
  const [query, setQuery] = useState('');
  const [inviting, setInviting] = useState(false);
  const [asking, setAsking] = useState<Asking>(null);
  const [sheetFor, setSheetFor] = useState<string | null>(null);

  const owners = members.filter((m) => m.role === 'owner').length;
  const shown = byRole(members).filter((m) => matches(m, query));
  const shownInvitations = invitations.filter((i) => i.email.includes(query.trim().toLowerCase()));
  const sheetMember = members.find((m) => m.user.id === sheetFor);
  const change = (m: Member) => roleChange(m, powers, owners, org.name);

  const actions: RowActions = {
    onFactoryRoles: (m) => setSheetFor(m.user.id),
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
        description={<p className={descriptionClass}>{countTitle(members.length, invitations.length)}</p>}
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

      <div className="grid gap-4 @[960px]:grid-cols-[minmax(0,1.8fr)_minmax(0,1fr)] @[960px]:items-start">
        <div className="flex min-w-0 flex-col gap-4">
          <SectionCard
            title="Members"
            aside={<SearchField value={query} onChange={setQuery} />}
            className="gap-3 p-4 md:p-5"
          >
            {/* From 720 px: a table. */}
            <table className="hidden w-full border-collapse text-left text-body @[720px]:table">
              <thead>
                <tr className="text-secondary text-muted [&>th]:border-b [&>th]:border-line [&>th]:px-3 [&>th]:pb-2 [&>th]:font-normal">
                  <th scope="col">Name</th>
                  <th scope="col">Role</th>
                  <th scope="col">Factories</th>
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
              </tbody>
            </table>

            {/* Below 720 px: person cards; each opens its details in a sheet. */}
            <ul className="m-0 flex list-none flex-col gap-2 p-0 @[720px]:hidden">
              {shown.map((m) => (
                <li key={m.member_id}>
                  <button
                    type="button"
                    onClick={() => setSheetFor(m.user.id)}
                    className="grid min-h-16 w-full cursor-pointer grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 rounded-control border border-line bg-card px-3 py-2 text-left text-ink transition-colors duration-200 hover:border-rule"
                  >
                    <Avatar initials={initialsOf(m.user)} size="card" />
                    <span className="min-w-0">
                      <span className="flex items-center gap-2 text-body font-medium">
                        <span className="truncate">{displayName(m.user)}</span>
                        {m.you ? <Chip>You</Chip> : null}
                      </span>
                      <span className="block text-secondary text-muted">
                        {orgRoleNames[m.role]}. {factorySummary(m, factories)}
                      </span>
                    </span>
                    <ChevronRight size={18} strokeWidth={1.5} aria-hidden="true" className="text-muted" />
                  </button>
                </li>
              ))}
            </ul>
            {shown.length === 0 && shownInvitations.length === 0 ? (
              <p className="m-0 py-3 text-body text-muted">Nobody matches “{query.trim()}”.</p>
            ) : null}
          </SectionCard>

          {invitations.length > 0 ? (
            <SectionCard
              title={
                <span className="flex items-center gap-2">
                  Waiting to join <Chip>{invitations.length}</Chip>
                </span>
              }
              className="gap-3 p-4 md:p-5"
            >
              <table className="hidden w-full border-collapse text-left text-body @[720px]:table">
                <thead>
                  <tr className="text-secondary text-muted [&>th]:border-b [&>th]:border-line [&>th]:px-3 [&>th]:pb-2 [&>th]:font-normal">
                    <th scope="col">Email</th>
                    <th scope="col">Role</th>
                    <th scope="col">Sent</th>
                    <th scope="col">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {shownInvitations.map((i) => (
                    <InvitationRow key={i.id} invitation={i} powers={powers} changes={changes} />
                  ))}
                </tbody>
              </table>
              <ul className="m-0 flex list-none flex-col gap-2 p-0 @[720px]:hidden">
                {shownInvitations.map((i) => (
                  <li key={i.id}>
                    <InvitationCard invitation={i} powers={powers} changes={changes} />
                  </li>
                ))}
              </ul>
            </SectionCard>
          ) : null}
        </div>

        <div className="flex min-w-0 flex-col gap-4">
          {powers.manages ? <InviteLinkCard /> : null}
          <CapabilitiesCard />
        </div>
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
    <p role="alert" className="m-0 text-body text-rework-ink">
      {refusalText(error, org)}
    </p>
  ) : null;
}

function SearchField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const id = useId();
  return (
    <div className="relative w-full @[720px]:w-56">
      <label htmlFor={id} className="sr-only">
        Search members
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
        placeholder="Search members"
        autoComplete="off"
        className="h-11 w-full rounded-control border border-rule bg-card pr-3 pl-9 text-body text-ink transition-colors duration-200 hover:border-muted md:h-8"
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
        <span className="flex items-center gap-2 text-body font-medium">
          <span className="truncate">{displayName(member.user)}</span>
          {member.you ? <span className="font-normal text-muted">(you)</span> : null}
        </span>
        {hasName(member.user) ? (
          <span className="block truncate text-secondary text-muted">{member.user.email}</span>
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
  if (!powers.manages) return <span className="text-body">{orgRoleNames[member.role]}</span>;
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
        <span id={reasonId} className="max-w-40 text-meta leading-snug text-muted">
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
    <tr className="[&>td]:border-b [&>td]:border-line [&>td]:px-3 [&>td]:py-3 [&>td]:align-middle">
      <th scope="row" className="border-b border-line px-3 py-3 text-left font-normal">
        <Person member={member} />
      </th>
      <td>
        <RoleControl member={member} org={org} change={change} powers={powers} actions={actions} />
      </td>
      <td className="text-secondary text-ink-soft">{factorySummary(member, factories)}</td>
      <td className="text-secondary text-muted">{lastActive(member.last_active_at)}</td>
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
        <Button variant="ghost" size="icon" aria-label={`More for ${displayName(member.user)}`} className="ml-auto">
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
        size="sm"
        aria-label={`Resend the invitation to ${invitation.email}`}
        disabled={busy(changes.resend)}
        onClick={() => changes.resend.mutate(invitation)}
      >
        Resend
      </Button>
      <Button
        variant="white"
        size="sm"
        className="text-rework-ink"
        aria-label={`Revoke the invitation to ${invitation.email}`}
        disabled={busy(changes.cancel)}
        onClick={() => changes.cancel.mutate(invitation)}
      >
        Revoke
      </Button>
    </span>
  );
}

function InvitationRow({ invitation, powers, changes }: { invitation: Invitation; powers: Powers; changes: Changes }) {
  return (
    <tr className="[&>td]:border-b [&>td]:border-line [&>td]:px-3 [&>td]:py-3 [&>td]:align-middle">
      <th scope="row" className="border-b border-line px-3 py-3 text-left font-normal">
        <span className="block truncate text-body">{invitation.email}</span>
      </th>
      <td>
        <Chip>{orgRoleNames[invitation.role]}</Chip>
      </td>
      <td className="text-secondary text-muted">{lastActive(invitation.created_at)}</td>
      <td>
        <InvitationActions invitation={invitation} powers={powers} changes={changes} />
      </td>
    </tr>
  );
}

function InvitationCard({ invitation, powers, changes }: { invitation: Invitation; powers: Powers; changes: Changes }) {
  return (
    <div className="flex flex-col gap-2 rounded-control border border-dashed border-rule bg-card px-3 py-2">
      <span className="flex min-w-0 items-center justify-between gap-3">
        <span className="min-w-0">
          <span className="block truncate text-body font-medium">{invitation.email}</span>
          <span className="block text-secondary text-muted">
            {orgRoleNames[invitation.role]}. Sent {lastActive(invitation.created_at).toLowerCase()}
          </span>
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
  if (factories.length === 0) return <p className="m-0 text-body text-muted">No factories yet.</p>;
  return (
    <ul className="m-0 flex list-none flex-col gap-2 p-0">
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
              <span className="text-body">{f.name}</span>
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

/** The invite link, with its controls. No invite link API exists yet: each control is not available. */
function InviteLinkCard() {
  return (
    <SectionCard title="Invite link" className="gap-3 p-4 md:p-5">
      <div className="flex h-9 min-w-0 items-center gap-2 rounded-control border border-rule bg-panel px-3">
        <span className="min-w-0 flex-1 truncate text-secondary text-muted">Not available yet</span>
        <NotAvailable label="Copy the invite link">
          <Button variant="white" size="sm">
            Copy
          </Button>
        </NotAvailable>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <span className="text-secondary text-ink-soft">Joins as</span>
        <NotAvailable label="Joins as">
          <Button variant="white" size="sm">
            Member
            <ChevronDown size={14} strokeWidth={1.75} aria-hidden="true" />
          </Button>
        </NotAvailable>
        <span className="ml-auto">
          <NotAvailable label="Reset the invite link">
            <Button variant="white" size="sm">
              Reset link
            </Button>
          </NotAvailable>
        </span>
      </div>
      <div className="flex items-start gap-3 border-t border-line pt-3">
        <span className="flex flex-1 flex-col gap-1">
          <span className="text-body font-medium">Only emails at a domain</span>
          <span className="text-secondary text-muted">Anyone at your domain joins as a Member</span>
        </span>
        <NotAvailable label="Restrict sign-up to a domain">
          <Switch checked={false} onCheckedChange={() => {}} aria-label="Restrict sign-up to a domain" />
        </NotAvailable>
      </div>
    </SectionCard>
  );
}

/** What each role can do, as a matrix. The API has three organization roles: the Member column is the Builder row of the screen. */
const capabilityRows: { can: string; owner: boolean; admin: boolean; member: boolean }[] = [
  { can: 'See floors, boards and items', owner: true, admin: true, member: true },
  { can: 'Create items, answer, approve', owner: true, admin: true, member: true },
  { can: 'Edit and publish definitions', owner: true, admin: true, member: true },
  { can: 'Connections and credentials', owner: true, admin: true, member: false },
  { can: 'Members, budgets, models', owner: true, admin: true, member: false },
  { can: 'Transfer or delete the organization', owner: true, admin: false, member: false },
];

function CapabilityMark({ allowed }: { allowed: boolean }) {
  return allowed ? (
    <>
      <Check size={16} strokeWidth={2} aria-hidden="true" className="mx-auto text-ok-ink" />
      <span className="sr-only">Yes</span>
    </>
  ) : (
    <>
      <Minus size={16} strokeWidth={2} aria-hidden="true" className="mx-auto text-rule" />
      <span className="sr-only">No</span>
    </>
  );
}

function CapabilitiesCard() {
  return (
    <SectionCard title="What each role can do" className="gap-3 p-4 md:p-5">
      <table className="w-full border-collapse text-left text-secondary">
        <thead>
          <tr className="text-muted [&>th]:border-b [&>th]:border-line [&>th]:px-1 [&>th]:pb-2 [&>th]:font-normal">
            <th scope="col">
              <span className="sr-only">Permission</span>
            </th>
            <th scope="col" className="text-center">
              {orgRoleNames.owner}
            </th>
            <th scope="col" className="text-center">
              {orgRoleNames.admin}
            </th>
            <th scope="col" className="text-center">
              {orgRoleNames.member}
            </th>
          </tr>
        </thead>
        <tbody>
          {capabilityRows.map((row) => (
            <tr
              key={row.can}
              className="[&>td]:border-b [&>td]:border-line [&>td]:px-1 [&>td]:py-2 [&>td]:align-middle"
            >
              <th scope="row" className="border-b border-line px-1 py-2 font-normal text-ink">
                {row.can}
              </th>
              <td className="text-center">
                <CapabilityMark allowed={row.owner} />
              </td>
              <td className="text-center">
                <CapabilityMark allowed={row.admin} />
              </td>
              <td className="text-center">
                <CapabilityMark allowed={row.member} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </SectionCard>
  );
}

/** A person's details: their organization role, factory roles, remove or leave. */
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
      <div className="flex flex-col gap-4 px-2 pb-2">
        <p className="m-0 flex items-center gap-3 text-body text-muted">
          <Avatar initials={initialsOf(member.user)} size="card" />
          <span className="min-w-0 break-words">
            {member.user.email},{' '}
            {member.last_active_at ? `active ${lastActive(member.last_active_at).toLowerCase()}` : 'never active'}
          </span>
        </p>
        <div className="flex flex-col gap-1">
          <span className="text-body font-medium">In {org.name}</span>
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
          <span className="text-body font-medium">In factories</span>
          <FactoryRoles member={member} factories={factories} powers={powers} onChange={onFactoryRole} stacked />
          {powers.grants && !editorEverywhere(member.role) ? (
            <p className="m-0 text-secondary text-muted">Changes apply at once and go into the audit log.</p>
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
