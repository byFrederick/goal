// The mock's people (spec 3, PR 3-11): who exists, who belongs to which organization with which
// role and factory roles, and the invitations, as Better Auth's rows and Goal's factory_roles keep
// them. Members and roles (canvas "Members"), Invite-Accept and Transfer ownership read and change
// them through goald's routes (api.ts) and the identity service's (auth.ts). The seed is the
// boards' Acme: an owner, an admin, two members with factory roles, two pending invitations.
import type { FactoryRole, Member, OrgRole } from '@goal/api-client';

export interface MockPerson {
  id: string;
  email: string;
  name: string;
  platformAdmin: boolean;
}

export interface MockMember {
  memberId: string;
  userId: string;
  role: OrgRole;
  joinedAt: number;
  /** Last activity (ms); null: never signed in. */
  lastActiveAt: number | null;
  /** Factory roles by factory id. */
  factoryRoles: Record<string, FactoryRole>;
}

export interface MockInvitation {
  id: string;
  orgId: string;
  email: string;
  role: OrgRole;
  factoryRoles: { factoryId: string; role: FactoryRole }[];
  inviterId: string;
  createdAt: number;
  expiresAt: number;
  status: 'pending' | 'accepted' | 'rejected' | 'canceled';
}

export interface MockPeople {
  people: MockPerson[];
  /** Members by organization id. */
  members: Record<string, MockMember[]>;
  invitations: MockInvitation[];
}

const dayMs = 86_400_000;
/** Invitations can be used for this long (the identity service's invitationExpiresIn). */
export const invitationLifetimeMs = 3 * dayMs;

const person = (n: number, email: string, name: string, platformAdmin = false): MockPerson => ({
  id: `019a1c00-0000-7000-8000-0000000d${String(n).padStart(4, '0')}`,
  email,
  name,
  platformAdmin,
});

/** The people of the seed by their email's local part. */
export const directory = {
  owner: person(1, 'f.cruz@example.com', 'F. Cruz', true),
  okafor: person(2, 'm.okafor@example.com', 'M. Okafor'),
  lindqvist: person(3, 'j.lindqvist@example.com', 'J. Lindqvist'),
  ortiz: person(4, 'a.ortiz@example.com', 'A. Ortiz'),
  lee: person(5, 's.lee@example.com', 'S. Lee'),
  nair: person(6, 'p.nair@example.com', 'P. Nair'),
  /** A user in no organization: every invitation of theirs was used or cancelled. */
  doe: person(7, 'j.doe@example.com', 'J. Doe'),
} as const;

/** The seed's invitations: acme's two pending ones, one that was cancelled, one to someone who joined. */
export const invitationIds = {
  pending: '019a1c00-0000-7000-8000-0000000e0001',
  second: '019a1c00-0000-7000-8000-0000000e0004',
  cancelled: '019a1c00-0000-7000-8000-0000000e0002',
  member: '019a1c00-0000-7000-8000-0000000e0003',
} as const;

interface SeedOrgs {
  acme: string;
  labs: string;
  /** acme's factories by name. */
  factories: { goal: string; sre: string; support: string };
}

export function seedPeople(now: number, { acme, labs, factories }: SeedOrgs): MockPeople {
  let n = 0;
  const member = (
    who: MockPerson,
    role: OrgRole,
    activeDaysAgo: number | null,
    factoryRoles: Record<string, FactoryRole> = {},
  ): MockMember => ({
    memberId: `019a1c00-0000-7000-8000-0000000f${String(++n).padStart(4, '0')}`,
    userId: who.id,
    role,
    joinedAt: now - 60 * dayMs,
    lastActiveAt: activeDaysAgo === null ? null : now - activeDaysAgo * dayMs,
    factoryRoles,
  });
  const d = directory;
  return {
    // Copies: a change (a new name) lasts until the next reset only.
    people: Object.values(directory).map((p) => ({ ...p })),
    members: {
      [acme]: [
        member(d.owner, 'owner', 0),
        member(d.okafor, 'admin', 0),
        member(d.lindqvist, 'member', 1, { [factories.goal]: 'editor', [factories.sre]: 'editor' }),
        member(d.ortiz, 'member', 2, { [factories.support]: 'viewer' }),
      ],
      [labs]: [member(d.owner, 'owner', 0), member(d.ortiz, 'member', 2)],
    },
    invitations: [
      {
        id: invitationIds.pending,
        orgId: acme,
        email: 'd.ross@example.com',
        role: 'member',
        factoryRoles: [{ factoryId: factories.sre, role: 'editor' }],
        inviterId: d.okafor.id,
        createdAt: now - 3 * dayMs,
        expiresAt: now + 4 * dayMs,
        status: 'pending',
      },
      {
        id: invitationIds.second,
        orgId: acme,
        email: 'ops@northwind.dev',
        role: 'member',
        factoryRoles: [{ factoryId: factories.support, role: 'viewer' }],
        inviterId: d.owner.id,
        createdAt: now - 60_000,
        expiresAt: now + invitationLifetimeMs,
        status: 'pending',
      },
      {
        id: invitationIds.cancelled,
        orgId: acme,
        email: 'd.ross@example.com',
        role: 'admin',
        factoryRoles: [],
        inviterId: d.owner.id,
        createdAt: now - 2 * dayMs,
        expiresAt: now + dayMs,
        status: 'canceled',
      },
      {
        // A pending invitation to someone who is a member already (they joined some other way).
        id: invitationIds.member,
        orgId: labs,
        email: d.ortiz.email,
        role: 'member',
        factoryRoles: [],
        inviterId: d.owner.id,
        createdAt: now - dayMs,
        expiresAt: now + dayMs,
        status: 'pending',
      },
    ],
  };
}

/** An organization's permissions by role (contracts/api/permissions.yaml), sorted, as goald answers Org.can. */
export function orgCanOf(role: OrgRole, every: readonly string[]): string[] {
  if (role === 'owner') return [...every];
  if (role === 'admin') return every.filter((p) => p !== 'org:delete' && p !== 'org:transfer');
  return ['member:read', 'org:read', 'personal_token:manage', 'personal_token:read'];
}

/**
 * The members of an organization as goald lists them for `caller` (a user id, or null for the local
 * user of sign-in off): by name, factory roles on the factories `visible` says the caller sees, and
 * what the caller may change (spec 3 §8.9 listMembers).
 */
export function membersFor(
  people: MockPeople,
  orgId: string,
  caller: string | null,
  visible: (factoryId: string) => boolean,
): Member[] {
  const members = people.members[orgId] ?? [];
  const me = members.find((m) => m.userId === caller);
  const owners = members.filter((m) => m.role === 'owner').length;
  const manages = me !== undefined && (me.role === 'owner' || me.role === 'admin');
  const named = members.flatMap((m) => {
    const who = people.people.find((p) => p.id === m.userId);
    return who ? [{ m, who }] : [];
  });
  return named
    .sort((a, b) => a.who.name.localeCompare(b.who.name))
    .map(({ m, who }) => {
      const changeable = manages && (m.role !== 'owner' || (me?.role === 'owner' && owners > 1));
      const you = m.userId === caller;
      return {
        member_id: m.memberId,
        user: { id: who.id, name: who.name, email: who.email },
        role: m.role,
        factory_roles: Object.entries(m.factoryRoles)
          .filter(([f]) => visible(f))
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([factory_id, role]) => ({ factory_id, role })),
        joined_at: new Date(m.joinedAt).toISOString(),
        last_active_at: m.lastActiveAt === null ? null : new Date(m.lastActiveAt).toISOString(),
        you,
        can_change_role: changeable,
        can_remove: changeable && !you,
      };
    });
}
