// What Members and roles says about people (canvas "Members", "Members-View", "Mobile-Members";
// spec 3 §10.2): roles as words, a member's factories, last activity by day, why a role cannot be
// changed, the invite dialog's addresses. Pure: the page renders what these return.
import type { InviteRequest, OrgRoleName } from '../../api/auth';
import type { FactoryRole, Invitation, Member, OrgRole } from '../../api/types';
import { formatDay } from '../../shared/format';
import { displayName } from '../../shared/names';

export const orgRoleNames: Record<OrgRole, string> = { owner: 'Owner', admin: 'Admin', member: 'Member' };
export const factoryRoleNames: Record<FactoryRole, string> = {
  viewer: 'Viewer',
  approver: 'Approver',
  editor: 'Editor',
};

/** The factory roles, lowest first: each includes the one before. */
export const factoryRoles: readonly FactoryRole[] = ['viewer', 'approver', 'editor'];

/** Owners and admins hold every factory role by their organization role. */
export const editorEverywhere = (role: OrgRole) => role === 'owner' || role === 'admin';

/** What a role on a factory allows, in a line under the person's name (People with access). */
export const factoryAccessLines: Record<Exclude<OrgRole, 'member'> | FactoryRole, string> = {
  owner: 'Organization owner',
  admin: 'Organization admin',
  editor: 'Edits the definition and creates items',
  approver: 'Approves and decides',
  viewer: 'Sees the floor, creates items',
};

/** Whether a member sees the factory: owners and admins hold every role, the others hold a role on it. */
export function seesFactory(m: Member, factory: string): boolean {
  return editorEverywhere(m.role) || m.factory_roles.some((g) => g.factory_id === factory);
}

/**
 * A member's factories in a line: "sre approver, support viewer" (by factory name, of the
 * factories the caller sees), "Editor in every factory" for owners and admins, "No factories".
 */
export function factorySummary(m: Member, factories: readonly { id: string; name: string }[]): string {
  if (editorEverywhere(m.role)) return 'Editor in every factory';
  const names = new Map(factories.map((f) => [f.id, f.name]));
  const held = m.factory_roles
    .flatMap((g) => {
      const name = names.get(g.factory_id);
      return name === undefined ? [] : [{ name, role: g.role }];
    })
    .sort((a, b) => a.name.localeCompare(b.name));
  return held.length > 0 ? held.map((g) => `${g.name} ${g.role}`).join(', ') : 'No factories';
}

/** "Today", "Yesterday", "Oct 20" in the person's time zone; "Never" without a sign-in. */
export function lastActive(at: string | null, now = new Date()): string {
  if (at === null) return 'Never';
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((day(now) - day(new Date(at))) / 86_400_000);
  if (days <= 0) return 'Today';
  if (days === 1) return 'Yesterday';
  return formatDay(at);
}

export interface RoleChange {
  /** The role can be changed from this page. */
  editable: boolean;
  /** Why it cannot, to someone who manages people; nothing to someone who only reads. */
  reason?: string;
}

/**
 * Whether the caller can change m's organization role, and if not why: goald says whether
 * (can_change_role), the rules it follows say why (only owners touch an owner; an organization
 * keeps an owner).
 */
export function roleChange(
  m: Member,
  viewer: { manages: boolean; role: OrgRole | null | undefined },
  owners: number,
  org: string,
): RoleChange {
  if (!viewer.manages) return { editable: false };
  if (m.can_change_role) return { editable: true };
  if (m.role === 'owner' && viewer.role !== 'owner') return { editable: false, reason: 'Only owners change an owner' };
  if (m.role === 'owner' && owners <= 1) return { editable: false, reason: `${org} needs at least one owner` };
  return { editable: false };
}

/** "6 members, 1 invite"; no invitations, no second part. */
export function countTitle(members: number, invitations: number): string {
  const people = `${members} ${members === 1 ? 'member' : 'members'}`;
  if (invitations === 0) return people;
  return `${people}, ${invitations} ${invitations === 1 ? 'invite' : 'invites'} waiting`;
}

/** "Invited Oct 24 by M. Okafor, expires Oct 27". */
export function invitationLine(i: Pick<Invitation, 'created_at' | 'expires_at' | 'inviter'>): string {
  return `Invited ${formatDay(i.created_at)} by ${i.inviter.name}, expires ${formatDay(i.expires_at)}`;
}

const rank: Record<OrgRole, number> = { owner: 0, admin: 1, member: 2 };

/** Owners first, then admins, then members, each by name (by email while they have none). */
export function byRole(members: readonly Member[]): Member[] {
  return [...members].sort(
    (a, b) => rank[a.role] - rank[b.role] || displayName(a.user).localeCompare(displayName(b.user)),
  );
}

/** A person matches the search when their name or email contains it, case aside. */
export function matches(m: Member, query: string): boolean {
  const q = query.trim().toLowerCase();
  return q === '' || m.user.name.toLowerCase().includes(q) || m.user.email.toLowerCase().includes(q);
}

const address = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** The invite dialog's addresses: one per line (or comma), trimmed, lower-cased, once each. */
export function parseEmails(text: string): { emails: string[]; invalid: string[] } {
  const entries = text
    .split(/[\n,;]+/)
    .map((e) => e.trim())
    .filter(Boolean);
  const emails = [...new Set(entries.filter((e) => address.test(e)).map((e) => e.toLowerCase()))];
  return { emails, invalid: entries.filter((e) => !address.test(e)) };
}

/** At most this many addresses at once (the invite dialog). */
export const maxInvites = 20;

/** Factory grants by factory id, as the invite dialog holds them: a role, or '' for none. */
export type FactoryGrants = Record<string, FactoryRole | ''>;

/** The grants that apply, as the invitation takes them: the factories with a role. */
export function factoryRolesOf(grants: FactoryGrants): InviteRequest['factoryRoles'] {
  return Object.entries(grants).flatMap(([factoryId, r]) => (r ? [{ factoryId, role: r }] : []));
}

/**
 * One invitation request per address, all with the same role. Only members get factory grants:
 * owners and admins hold every factory role already. The invite dialog and onboarding send these.
 */
export function inviteRequestsOf({
  orgId,
  emails,
  role,
  grants,
}: {
  orgId: string;
  emails: readonly string[];
  role: OrgRoleName;
  grants: FactoryGrants;
}): InviteRequest[] {
  const factoryRoles = role === 'member' ? factoryRolesOf(grants) : [];
  return emails.map((email) => ({ organizationId: orgId, email, role, factoryRoles }));
}
