// The mock's caller in a role (spec 3 §4.2), so screens and Playwright can check each one. A
// reset with {role: "owner" | "admin" | "member" | "editor" | "approver" | "viewer"} makes the
// caller that member of every organization: editor, approver and viewer are members with that
// role on every factory; a member has none, so no factory is visible to them; owners and admins
// act as editors everywhere. Without a role the mock is the local user of GOAL_AUTH=none: every
// permission, no roles named.
//
// Both tables are goald's own sources, read once: contracts/api/permissions.yaml says who holds what, and
// contracts/api/openapi.yaml's x-goal-scope which permission each operation needs. The mock refuses an
// operation the caller lacks as goald does (403 forbidden), so a screen that offered it anyway
// meets the same refusal it would meet against goald.
import { readFileSync } from 'node:fs';
import type { FactoryRole, OrgRole } from '@goal/api-client';
import { parse } from 'yaml';

export const mockRoles = ['owner', 'admin', 'member', 'editor', 'approver', 'viewer'] as const;
export type MockRole = (typeof mockRoles)[number];

/** The role of a reset's `role` member; undefined (the local user) without a known one. */
export function roleSetting(body: unknown): MockRole | undefined {
  return mockRoles.find((r) => r === body);
}

interface Rule {
  name: string;
  level: 'install' | 'org' | 'factory';
  org_roles?: OrgRole[];
  factory_role?: FactoryRole;
}

export interface PermissionTable {
  rules: readonly Rule[];
  factoryRoles: readonly FactoryRole[];
}

/** contracts/api/permissions.yaml's text as the table. */
export function permissionTable(text: string): PermissionTable {
  const doc = parse(text) as { factory_roles: FactoryRole[]; permissions: Rule[] };
  return { rules: doc.permissions, factoryRoles: doc.factory_roles };
}

export interface Grants {
  orgRole: OrgRole;
  /** The effective factory role on every factory (editor for owners and admins); null: none. */
  factoryRole: FactoryRole | null;
  /** Org.can and Factory.can, sorted as goald sorts them. */
  org: string[];
  factory: string[];
}

/** What `role` holds by the table (owners and admins hold every factory permission). */
export function grantsOf(table: PermissionTable, role: MockRole): Grants {
  const orgRole: OrgRole = role === 'owner' || role === 'admin' ? role : 'member';
  const factoryRole: FactoryRole | null =
    orgRole !== 'member' ? 'editor' : role === 'member' ? null : (role as FactoryRole);
  const rank = (r: FactoryRole | undefined) => (r ? table.factoryRoles.indexOf(r) : -1);
  const org = table.rules.filter((r) => r.level === 'org' && r.org_roles?.includes(orgRole)).map((r) => r.name);
  const factory = table.rules
    .filter((r) => r.level === 'factory' && factoryRole !== null && rank(r.factory_role) <= rank(factoryRole))
    .map((r) => r.name);
  return { orgRole, factoryRole, org: org.sort(), factory: factory.sort() };
}

export interface Scope {
  method: string;
  pattern: RegExp;
  /** The path parameters' names, in the pattern's group order. */
  params: string[];
  permission: string;
}

/** Each operation of contracts/api/openapi.yaml's text: its method, path and x-goal-scope. */
export function scopesFrom(text: string): Scope[] {
  const doc = parse(text) as { paths: Record<string, Record<string, { 'x-goal-scope'?: string }>> };
  const scopes: Scope[] = [];
  for (const [path, item] of Object.entries(doc.paths)) {
    const params: string[] = [];
    const source = path
      .split(/(\{[^}]+\})/)
      .map((part) => {
        const name = /^\{([^}]+)\}$/.exec(part)?.[1];
        if (!name) return part.replace(/[.*+?^$()|[\]\\]/g, '\\$&');
        params.push(name);
        return '([^/]+)';
      })
      .join('');
    for (const [method, op] of Object.entries(item)) {
      const permission = op['x-goal-scope'];
      if (permission)
        scopes.push({ method: method.toUpperCase(), pattern: new RegExp(`^${source}$`), params, permission });
    }
  }
  return scopes;
}

/** The permission a request needs, with the path's parameters (decoded); undefined for no operation. */
export function scopeOf(
  scopes: readonly Scope[],
  method: string,
  path: string,
): { permission: string; params: Record<string, string> } | undefined {
  for (const s of scopes) {
    if (s.method !== method) continue;
    const m = s.pattern.exec(path);
    if (!m) continue;
    const params = Object.fromEntries(s.params.map((name, i) => [name, decodeURIComponent(m[i + 1] ?? '')]));
    return { permission: s.permission, params };
  }
  return undefined;
}

/** The level of each permission, from the table. */
export function levelsOf(table: PermissionTable): ReadonlyMap<string, Rule['level']> {
  return new Map(table.rules.map((r) => [r.name, r.level]));
}

/** goald's two sources, read from the repository the mock runs in. */
export function readSources(): { table: PermissionTable; scopes: Scope[] } {
  const read = (file: string) => readFileSync(new URL(`../../contracts/api/${file}`, import.meta.url), 'utf8');
  return { table: permissionTable(read('permissions.yaml')), scopes: scopesFrom(read('openapi.yaml')) };
}
