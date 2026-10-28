// The organization the screens act in: the one the URL names (/o/$org, by slug or id), resolved
// by the /o/$org layout before anything under it loads, and the one /account shows (the last one
// opened). It reaches the screens through OrgContext, which both routes provide. The last one opened
// is remembered so `/` can return to it.
import { useQuery } from '@tanstack/react-query';
import { useMatch } from '@tanstack/react-router';
import { createContext, use } from 'react';
import { orgQuery } from '../api/queries';
import type { Org, OrgSummary } from '../api/types';
import { holds, type OrgPermission } from './permissions';

/** The organization a page under an organization shows. Query keys are rooted at its `id`; links use its `slug`. */
export const OrgContext = createContext<OrgSummary | null>(null);

/**
 * The current organization: from OrgContext (/account), or from the /o/$org route that matched (the
 * organization pages' layout also provides it).
 */
export function useOrg(): OrgSummary {
  const provided = use(OrgContext);
  const routed = useMatch({ from: '/o/$org', shouldThrow: false, select: (m) => m?.context.org });
  const org = provided ?? routed;
  if (!org) throw new Error('useOrg outside an organization page');
  return org;
}

/**
 * The current organization as goald answers it to the caller: their role and `can`, what the
 * screens may offer (holds() in permissions.ts). Loaded by the /o/$org layout; undefined only
 * while a refetch after an error is pending.
 */
export function useOrgGrants(): Org | undefined {
  return useQuery(orgQuery(useOrg().id)).data;
}

/** Whether the caller holds an organization permission in the current organization (its `can`). */
export function useOrgHolds(permission: OrgPermission): boolean {
  return holds(useOrgGrants(), permission);
}

/** The organization matching a URL's $org: its slug or its id. */
export function findOrg(orgs: readonly OrgSummary[], key: string): OrgSummary | undefined {
  return orgs.find((o) => o.slug === key || o.id === key);
}

const lastOrgKey = 'goal.org';
/** Where the UI remembered its factory before organizations; removed on the first visit (C §14). */
const legacyFactoryKey = 'goal.factory';

export function rememberOrg(slug: string): void {
  try {
    localStorage.setItem(lastOrgKey, slug);
    localStorage.removeItem(legacyFactoryKey);
  } catch {
    // Storage can be unavailable (private windows); `/` then opens the first organization.
  }
}

/** The slug of the organization used last, if storage has one. */
export function lastOrgSlug(): string | null {
  try {
    return localStorage.getItem(lastOrgKey);
  } catch {
    return null;
  }
}

/** The organization `/` opens: the one used last, else the first goald lists. */
export function defaultOrg(orgs: readonly OrgSummary[]): OrgSummary | undefined {
  const last = lastOrgSlug();
  return (last ? findOrg(orgs, last) : undefined) ?? orgs[0];
}

const lastFactoryKey = (orgId: string) => `goal.factory.${orgId}`;

/** Remembers the factory a person last opened in an organization (by its id), for the phone's tab bar. */
export function rememberFactory(orgId: string, factoryId: string): void {
  try {
    localStorage.setItem(lastFactoryKey(orgId), factoryId);
  } catch {
    // Storage can be unavailable; the tab bar then falls back to the first factory.
  }
}

/**
 * The factory last opened in `orgId`, if it is still in `factories` and not archived; otherwise
 * undefined. The list is the organization's active factories, so an archived one never matches.
 */
export function lastFactory<F extends { id: string; archived_at?: string | null }>(
  orgId: string,
  factories: readonly F[],
): F | undefined {
  let id: string | null;
  try {
    id = localStorage.getItem(lastFactoryKey(orgId));
  } catch {
    return undefined;
  }
  if (!id) return undefined;
  return factories.find((f) => f.id === id && !f.archived_at);
}

/** "3 factories", "1 factory". */
export function factoryCount(n: number): string {
  return `${n} ${n === 1 ? 'factory' : 'factories'}`;
}
