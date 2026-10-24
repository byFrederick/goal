// The organization the screens act in: the one the URL names (/o/$org, by slug or id), resolved
// by the /o/$org layout before anything under it loads. The last one opened is remembered so `/`
// can return to it.
import { useRouteContext } from '@tanstack/react-router';
import type { OrgSummary } from '../api/types';

/** The organization of the current /o/$org route. Query keys are rooted at its `id`; links use its `slug`. */
export function useOrg(): OrgSummary {
  return useRouteContext({ from: '/o/$org' }).org;
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

/** "3 factories", "1 factory". */
export function factoryCount(n: number): string {
  return `${n} ${n === 1 ? 'factory' : 'factories'}`;
}
