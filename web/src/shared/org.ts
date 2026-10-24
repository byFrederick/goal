// The organization the screens act in. Interim first-org shim (A §10): until C's /o/:org routes
// name the organization in the URL, the UI works in the first one goald lists (by name; the
// "default" organization after the upgrade). C3 replaces this module with the route's org.
import type { QueryClient } from '@tanstack/react-query';
import { useLoaderData } from '@tanstack/react-router';
import { orgsQuery } from '../api/queries';
import type { OrgSummary } from '../api/types';

/** The organization the UI shows: the first goald lists, or undefined when there is none. */
export async function interimOrg(client: QueryClient): Promise<OrgSummary | undefined> {
  const orgs = await client.ensureQueryData(orgsQuery);
  return orgs[0];
}

/** The id of the organization of the current /f/$factory route; query keys are rooted at it. */
export function useOrg(): string {
  return useLoaderData({ from: '/f/$factory' }).org.id;
}
