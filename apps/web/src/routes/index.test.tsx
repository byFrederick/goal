// @vitest-environment jsdom
import { QueryClient } from '@tanstack/react-query';
import { isRedirect } from '@tanstack/react-router';
import { describe, expect, it } from 'vitest';
import { meQuery, orgsQuery } from '../api/queries';
import type { Me, OrgSummary } from '../api/types';
import { Route } from './index';

const goal: OrgSummary = { id: 'org-goal', slug: 'goal', name: 'Goal', mark: 'Go', factory_count: 1, role: 'owner' };

const signedIn = {
  kind: 'user',
  mode: 'session',
  platform_admin: false,
  user: { id: 'user-1', name: 'Freddy Cruz', email: 'freddy@example.com' },
} as Me;

/** The loader of `/`, run against a cache that already holds the organizations and the person. */
async function openRoot(orgs: OrgSummary[], me: Me) {
  const queryClient = new QueryClient();
  queryClient.setQueryData(orgsQuery.queryKey, orgs);
  queryClient.setQueryData(meQuery.queryKey, me);
  const loader = Route.options.loader as unknown as (args: { context: { queryClient: QueryClient } }) => Promise<void>;
  return loader({ context: { queryClient } }).then(
    () => undefined,
    (err: unknown) => err,
  );
}

describe('/', () => {
  it('sends a signed-in person with no organization to onboarding', async () => {
    const outcome = await openRoot([], signedIn);

    expect(isRedirect(outcome)).toBe(true);
    expect(outcome).toMatchObject({ options: { to: '/get-started', replace: true } });
  });

  it('opens the organization used last, or the first one, before onboarding', async () => {
    const outcome = await openRoot([goal], signedIn);

    expect(outcome).toMatchObject({ options: { to: '/o/$org', params: { org: 'goal' } } });
  });

  it('stays on the page that says how to make one, with sign-in off', async () => {
    const outcome = await openRoot([], { kind: 'user', mode: 'none', platform_admin: false } as Me);

    expect(outcome).toBeUndefined();
  });
});
