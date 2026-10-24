// What the caller may do, as goald says it (spec 3 §10.1 "Role-aware UI"): Org.can and
// Factory.can list the permissions held, so screens offer a control only when its permission is
// there and say why when they leave it disabled. Screens decide from `can`, never from role names
// (the role only names the person's place: "You're a viewer of sre"). goald checks every request
// again; a control left enabled by a stale `can` meets a 403, and the session handler refetches
// what `can` comes from (main.tsx). Under GOAL_AUTH=none `can` lists every permission.
import type { FactoryRole } from '../api/types';

/** The permissions the screens ask about (api/permissions.yaml). */
export type OrgPermission =
  | 'org:manage'
  | 'org:delete'
  | 'factory:create'
  | 'member:manage'
  | 'api_key:read'
  | 'api_key:manage';
export type FactoryPermission =
  | 'item:create'
  | 'item:decide'
  | 'item:operate'
  | 'andon:manage'
  | 'definition:edit'
  | 'definition:publish';

/** Anything goald answers `can` on. */
interface Grants {
  can?: readonly string[] | undefined;
}

/**
 * Whether goald lists `permission` for the caller. Nothing known (still loading, an answer without
 * `can`) holds nothing: a control waits for goald rather than offering what may be refused.
 */
export function holds(subject: Grants | undefined, permission: OrgPermission | FactoryPermission): boolean {
  return subject?.can?.includes(permission) ?? false;
}

/** Why a control of factory `factory` (its name) is disabled or absent. */
export const reasons = {
  decide: (factory: string) => `Approvers of ${factory} decide here.`,
  note: (factory: string) => `Approvers and editors of ${factory} send notes to its agents.`,
  andon: (pulled: boolean) =>
    pulled ? 'Approvers and editors can clear the andon' : 'Approvers and editors can pull the cord',
  board: (factory: string) =>
    `You can see ${factory}’s board. Approvers and editors of ${factory} move cards and decide items; agents pull work, nobody assigns it.`,
  definition: (factory: string) => `View only. Editors of ${factory} change its definition.`,
  publish: (factory: string) => `Editors of ${factory} review and publish its definition.`,
  settings: (org: string) => `View only. Owners and admins of ${org} change its settings.`,
  newFactory: (org: string) => `Owners and admins of ${org} create factories.`,
} as const;

const article = (role: string) => (/^[aeiou]/.test(role) ? 'an' : 'a');

/**
 * "You're a viewer of sre": the caller's factory role when it holds less than an editor's, else
 * null (editors, owners and admins, and GOAL_AUTH=none, where goald names no role).
 */
export function factoryRoleNote(role: FactoryRole | null | undefined, factory: string): string | null {
  if (role !== 'viewer' && role !== 'approver') return null;
  return `You’re ${article(role)} ${role} of ${factory}`;
}
