// What a personal token can do, in the New token dialog's words (spec 3 §4.5, §10.2 "Personal
// tokens"): the permissions personal tokens may carry (api/permissions.yaml `pat: true`), grouped
// Read / Work / High impact. High-impact scopes (editing drafts, publishing versions, creating
// and archiving factories) are never in a preset: publishing controls the prompts agents run and the repository
// they push to. A token acts as its owner, so it can never do more than their role allows, and
// it has no ceiling or factory restriction of its own.
import type { NewPersonalToken } from '../../api/types';
import type { Scope } from './scopes';

export type TokenGroupId = 'read' | 'work' | 'high';

export const tokenScopeGroups: readonly { id: TokenGroupId; label: string; scopes: readonly Scope[] }[] = [
  {
    id: 'read',
    label: 'Read',
    scopes: [
      { id: 'org:read', label: 'See the organization', needs: null },
      { id: 'factory:read', label: 'See factories', needs: 'viewer' },
      { id: 'connection:read', label: 'See connections', needs: null },
      { id: 'knowledge:read', label: 'Search the knowledge base', needs: null },
    ],
  },
  {
    id: 'work',
    label: 'Work',
    scopes: [
      { id: 'item:create', label: 'Submit items', needs: 'approver' },
      { id: 'item:decide', label: 'Decide items', needs: 'approver' },
      { id: 'item:operate', label: 'Move and pause items', needs: 'approver' },
      { id: 'andon:manage', label: 'Pull and clear the andon', needs: 'approver' },
    ],
  },
  {
    id: 'high',
    label: 'High impact',
    scopes: [
      { id: 'definition:edit', label: 'Edit drafts', needs: 'editor' },
      { id: 'definition:publish', label: 'Publish versions', needs: 'editor' },
      { id: 'factory:create', label: 'Create factories', needs: null },
      { id: 'factory:archive', label: 'Archive factories', needs: null },
    ],
  },
];

/** The scopes a token may carry, every group's. */
export const tokenScopes: readonly string[] = tokenScopeGroups.flatMap((g) => g.scopes.map((s) => s.id));

/** The high-impact scopes: never in a preset, warned about in the dialog. */
export const highImpactScopes: readonly string[] =
  tokenScopeGroups.find((g) => g.id === 'high')?.scopes.map((s) => s.id) ?? [];

/** "CLI: work": everything a person does from the CLI day to day; never a high-impact scope. */
export const cliWorkPreset: readonly string[] = tokenScopeGroups
  .filter((g) => g.id === 'read' || g.id === 'work')
  .flatMap((g) => g.scopes.map((s) => s.id));

export const tokenExpiryDays = [7, 30, 60, 90] as const;
export type TokenExpiryDays = (typeof tokenExpiryDays)[number];

/** The dialog's choice. It starts empty every time: nothing comes from the URL or a link. */
export interface TokenForm {
  name: string;
  days: TokenExpiryDays;
  scopes: string[];
}

export const emptyTokenForm: TokenForm = { name: '', days: 30, scopes: [] };

/** What is missing before a token can be created, by field; empty when it can. */
export function tokenFormErrors(form: TokenForm): Partial<Record<'name' | 'scopes', string>> {
  const errors: Partial<Record<'name' | 'scopes', string>> = {};
  const name = form.name.trim();
  if (!name) errors.name = 'Give the token a name';
  else if (name.length > 80) errors.name = 'At most 80 characters';
  if (form.scopes.length === 0) errors.scopes = 'Choose at least one thing it can do';
  return errors;
}

/** The choice as goald takes it: only scopes tokens may carry, in the table's order. */
export function newTokenBody(form: TokenForm): NewPersonalToken {
  return {
    name: form.name.trim(),
    scopes: tokenScopes.filter((s) => form.scopes.includes(s)),
    expires_in_days: form.days,
  };
}

/**
 * Whether the person holds `scope` now, as a token they create must (goald refuses one they do
 * not with scope-not-allowed): an organization permission in the organization's `can`, a factory
 * permission in at least one factory's.
 */
export function holdsScope(
  scope: Scope,
  orgCan: readonly string[] | undefined,
  factories: readonly { can: readonly string[] }[],
): boolean {
  if (scope.needs === null) return orgCan?.includes(scope.id) ?? false;
  return factories.some((f) => f.can.includes(scope.id));
}
