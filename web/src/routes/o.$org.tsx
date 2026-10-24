import { createFileRoute, notFound, Outlet } from '@tanstack/react-router';
import { useEffect } from 'react';
import { ensureLoaded, factoriesQuery, orgsQuery } from '../api/queries';
import { AppShell } from '../app/app-shell';
import { NotFoundScreen } from '../app/states';
import { findOrg, rememberOrg } from '../shared/org';
import { OrgStream } from '../shared/stream';

// Every page of one organization: the rail (or the phone's top row) and one live stream for the
// organization. $org is its slug (or id); everything under it keys its queries by the id.
export const Route = createFileRoute('/o/$org')({
  beforeLoad: async ({ context, params }) => {
    const orgs = await ensureLoaded(context.queryClient, orgsQuery);
    const org = findOrg(orgs, params.org);
    if (!org) throw notFound();
    return { org };
  },
  // The rail lists the organization's factories on every page.
  loader: ({ context: { queryClient, org } }) => ensureLoaded(queryClient, factoriesQuery(org.id)),
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
      <AppShell org={org}>
        <Outlet />
      </AppShell>
    </OrgStream>
  );
}
