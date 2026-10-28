import { createFileRoute, notFound, Outlet } from '@tanstack/react-router';
import { useEffect } from 'react';
import { ensureFound, ensureLoaded, factoriesQuery, meQuery, orgQuery, orgsQuery } from '../api/queries';
import { AppShell } from '../app/app-shell';
import { NotFoundScreen } from '../app/states';
import { findOrg, OrgContext, rememberOrg } from '../shared/org';
import { OrgStream } from '../shared/stream';

// Every page of one organization: the sidebar (or the phone's top row) and one live stream for the
// organization. $org is its slug (or id); everything under it keys its queries by the id.
export const Route = createFileRoute('/o/$org')({
  beforeLoad: async ({ context, params }) => {
    const org = await ensureFound(context.queryClient, orgsQuery, (orgs) => findOrg(orgs, params.org));
    if (!org) throw notFound();
    return { org };
  },
  // The sidebar lists the organization's factories on every page, offers New factory by what the
  // caller may do (`can`), and names the person (the account; a goald that cannot say, an older
  // one, leaves it the local user rather than failing the page).
  loader: ({ context: { queryClient, org } }) =>
    Promise.all([
      ensureLoaded(queryClient, factoriesQuery(org.id)),
      ensureLoaded(queryClient, orgQuery(org.id)),
      ensureLoaded(queryClient, meQuery).catch(() => undefined),
    ]),
  component: OrgLayout,
  notFoundComponent: () => (
    <main id="main" className="p-6">
      <NotFoundScreen what="organization" />
    </main>
  ),
});

function OrgLayout() {
  const { org } = Route.useRouteContext();

  useEffect(() => rememberOrg(org.slug), [org.slug]);

  return (
    <OrgStream org={org.id}>
      <OrgContext value={org}>
        <AppShell org={org}>
          <Outlet />
        </AppShell>
      </OrgContext>
    </OrgStream>
  );
}
