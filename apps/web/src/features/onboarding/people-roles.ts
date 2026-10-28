// Step 7's roles, as docs/design/screens/get-started/invite.jpg labels them (ADR 0032 decision 2). Owner and Admin
// are the organization's roles. Builder and Viewer are members with the editor or the viewer role on the new
// factory: the labels are the picture's, the API's role names stay.
import type { OrgRoleName } from '../../api/auth';
import type { FactoryRole, Invitation } from '../../api/types';
import { factoryRoleNames } from '../members/people';

export type InviteRole = 'owner' | 'admin' | 'builder' | 'viewer';

/** The four roles in the picture's order, each with the sentence the picture gives it. */
export const inviteRoles: readonly { value: InviteRole; label: string; description: string }[] = [
  { value: 'owner', label: 'Owner', description: 'Everything, including billing' },
  { value: 'admin', label: 'Admin', description: 'Factories, people and connections' },
  { value: 'builder', label: 'Builder', description: 'Definitions, items and approvals' },
  { value: 'viewer', label: 'Viewer', description: 'Sees everything, changes nothing' },
];

/** The organization role an invite role sends, and the factory role it gives on the new factory (members only). */
export function roleGrant(role: InviteRole): { role: OrgRoleName; grant: FactoryRole | null } {
  switch (role) {
    case 'owner':
      return { role: 'owner', grant: null };
    case 'admin':
      return { role: 'admin', grant: null };
    case 'builder':
      return { role: 'member', grant: 'editor' };
    case 'viewer':
      return { role: 'member', grant: 'viewer' };
  }
}

/** A pending invitation's role in the picture's words: "builder" for an editor of the factory, "owner" for an owner. */
export function inviteRoleWord(i: Pick<Invitation, 'role' | 'factory_roles'>): string {
  if (i.role !== 'member') return i.role;
  const grant = i.factory_roles[0]?.role;
  if (grant === undefined) return 'member';
  if (grant === 'editor') return 'builder';
  if (grant === 'viewer') return 'viewer';
  return factoryRoleNames[grant].toLowerCase();
}
