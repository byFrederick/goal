// What an API key can do, in the New key dialog's words (spec 3 §10.2 "Api-Keys"): the
// permissions keys may carry (contracts/api/permissions.yaml `keys: true`), grouped Read / Work / Build,
// the lowest factory role each factory permission needs (the key's ceiling must hold it), and the
// presets. High-impact scopes (editing and publishing definitions, creating and archiving factories) are never
// in a preset but "Publish definitions", which names them.
import type { FactoryRole, NewApiKey } from '../../api/types';
import { formatAge, formatDay } from '../../shared/format';

export interface Scope {
  id: string;
  label: string;
  /** The lowest factory role holding it; null for an organization permission. */
  needs: FactoryRole | null;
}

export const scopeGroups: readonly { id: 'read' | 'work' | 'build'; label: string; scopes: readonly Scope[] }[] = [
  {
    id: 'read',
    label: 'Read',
    scopes: [
      { id: 'org:read', label: 'See the organization', needs: null },
      { id: 'factory:read', label: 'See factories', needs: 'viewer' },
      { id: 'audit:read', label: 'Read the audit log', needs: null },
      { id: 'connection:read', label: 'See connections', needs: null },
      { id: 'connection:browse', label: 'List the repositories a connection reaches', needs: null },
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
    id: 'build',
    label: 'Build',
    scopes: [
      { id: 'definition:edit', label: 'Edit drafts', needs: 'editor' },
      { id: 'definition:publish', label: 'Publish versions', needs: 'editor' },
      { id: 'factory:manage', label: 'Pause factories', needs: 'editor' },
      { id: 'factory:create', label: 'Create factories', needs: null },
      { id: 'factory:archive', label: 'Archive factories', needs: null },
      { id: 'factory:delete', label: 'Delete factories', needs: null },
      { id: 'knowledge:manage', label: 'Manage the knowledge base', needs: null },
    ],
  },
];

const allScopes = scopeGroups.flatMap((g) => g.scopes);

/** A scope's words: "Submit items"; a scope this UI does not know by its id. */
export function scopeLabel(id: string): string {
  return allScopes.find((s) => s.id === id)?.label ?? id;
}

export const ceilings: readonly { value: FactoryRole; label: string; description: string }[] = [
  { value: 'viewer', label: 'Viewer', description: 'Sees factories, items and definitions' },
  { value: 'approver', label: 'Approver', description: 'Also submits, decides and moves work' },
  { value: 'editor', label: 'Editor', description: 'Also edits and publishes definitions' },
];

const ladder: readonly FactoryRole[] = ['viewer', 'approver', 'editor'];

/** Whether a key acting at most as `ceiling` can use scope (organization scopes always). */
export function allows(ceiling: FactoryRole, scope: Scope): boolean {
  return scope.needs === null || ladder.indexOf(scope.needs) <= ladder.indexOf(ceiling);
}

/** "Needs Editor": why a scope is off at this ceiling; undefined when it is on. */
export function needsText(ceiling: FactoryRole, scope: Scope): string | undefined {
  if (allows(ceiling, scope) || !scope.needs) return undefined;
  return `Needs ${ceilings.find((c) => c.value === scope.needs)?.label}`;
}

/** The chosen scopes a key at `ceiling` can still use: lowering it drops the ones above. */
export function withinCeiling(scopes: readonly string[], ceiling: FactoryRole): string[] {
  return scopes.filter((id) => {
    const s = allScopes.find((x) => x.id === id);
    return s ? allows(ceiling, s) : false;
  });
}

export type PresetId = 'read' | 'ci' | 'publish';

export const presets: readonly { id: PresetId; label: string; ceiling: FactoryRole; scopes: readonly string[] }[] = [
  { id: 'read', label: 'Read only', ceiling: 'viewer', scopes: ['org:read', 'factory:read'] },
  { id: 'ci', label: 'CI: submit work', ceiling: 'approver', scopes: ['org:read', 'factory:read', 'item:create'] },
  {
    id: 'publish',
    label: 'Publish definitions',
    ceiling: 'editor',
    scopes: ['org:read', 'factory:read', 'definition:edit', 'definition:publish'],
  },
];

/** The preset the choice is exactly, if any. */
export function presetOf(ceiling: FactoryRole, scopes: readonly string[]): PresetId | undefined {
  const chosen = [...scopes].sort().join();
  return presets.find((p) => p.ceiling === ceiling && [...p.scopes].sort().join() === chosen)?.id;
}

export const expiryDays = [30, 60, 90] as const;
export type ExpiryDays = (typeof expiryDays)[number];

/** The dialog's choice as goald takes it; `factories` null is every factory, now and later. */
export interface KeyForm {
  name: string;
  days: ExpiryDays;
  factories: string[] | null;
  ceiling: FactoryRole;
  scopes: string[];
}

export const emptyKeyForm: KeyForm = { name: '', days: 90, factories: null, ceiling: 'approver', scopes: [] };

/** What is missing before a key can be created, by field; empty when it can. */
export function keyFormErrors(form: KeyForm): Partial<Record<'name' | 'factories' | 'scopes', string>> {
  const errors: Partial<Record<'name' | 'factories' | 'scopes', string>> = {};
  const name = form.name.trim();
  if (!name) errors.name = 'Give the key a name';
  else if (name.length > 80) errors.name = 'At most 80 characters';
  if (form.factories !== null && form.factories.length === 0) errors.factories = 'Choose at least one factory';
  if (form.scopes.length === 0) errors.scopes = 'Choose at least one thing it can do';
  return errors;
}

export function newKeyBody(form: KeyForm): NewApiKey {
  return {
    name: form.name.trim(),
    scopes: withinCeiling(form.scopes, form.ceiling),
    role_ceiling: form.ceiling,
    expires_in_days: form.days,
    ...(form.factories ? { factories: form.factories } : {}),
  };
}

const dayMs = 86_400_000;

/** When a key expires, as its row says it: "Dec 31", and "in 9 days" under 14 days. */
export function expiryText(expiresAt: string, now: number): { day: string; soon: string | undefined } {
  const left = Date.parse(expiresAt) - now;
  const days = Math.ceil(left / dayMs);
  const soon = left <= 0 ? 'expired' : days < 14 ? (days === 1 ? 'in 1 day' : `in ${days} days`) : undefined;
  return { day: formatDay(expiresAt), soon };
}

/** "2 h ago", "Never". */
export function lastUsedText(lastUsedAt: string | null, now: number): string {
  if (!lastUsedAt) return 'Never';
  const age = formatAge(Math.max(0, (now - Date.parse(lastUsedAt)) / 1000));
  return age === 'now' ? 'Just now' : `${age} ago`;
}
