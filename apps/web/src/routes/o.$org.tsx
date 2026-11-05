import { createFileRoute, type NotFoundRouteProps, notFound, Outlet } from '@tanstack/react-router';
import { useEffect } from 'react';
import { ensureFound, ensureLoaded, factoriesQuery, meQuery, orgQuery, orgsQuery } from '../api/queries';
import { AppShell, ShellFrame } from '../app/app-shell';
import { ShellPending } from '../app/shell-pending';
import { NotFoundScreen, unknownFactory } from '../app/states';
import { TopBarSlot } from '../app/top-bar';
import { findOrg, OrgContext, rememberOrg } from '../shared/org';
import { OrgStream } from '../shared/stream';

/** Marks the not-found of an unknown slug, so an unknown page inside a known organization says so instead. */
const unknownOrg = { unknown: 'organization' } as const;

// Every page of one organization: the sidebar (or the phone's top row) and one live stream for the
// organization. $org is its slug (or id); everything under it keys its queries by the id.
export const Route = createFileRoute('/o/$org')({
  beforeLoad: async ({ context, params }) => {
    const org = await ensureFound(context.queryClient, orgsQuery, (orgs) => findOrg(orgs, params.org));
    if (!org) throw notFound({ data: unknownOrg });
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
  pendingComponent: ShellPending,
  component: OrgLayout,
  notFoundComponent: OrgNotFound,
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

// An unknown slug has no organization to show, so it stands alone. Any other unmatched path renders
// here in place of the page, inside OrgLayout's AppShell (the sidebar) but outside the pages' own
// layouts, so it draws the shell's frame itself: the top bar and the main region with the Route change motion.
function OrgNotFound({ data }: NotFoundRouteProps) {
  if ((data as { unknown?: string } | undefined)?.unknown === unknownOrg.unknown) {
    return (
      <main id="main" className="flex min-h-svh flex-col px-7">
        <NotFoundScreen what="organization" />
      </main>
    );
  }
  const factory = (data as { unknown?: string } | undefined)?.unknown === unknownFactory.unknown;
  return (
    <ShellFrame>
      <TopBarSlot crumb="Not found" />
      <NotFoundScreen what={factory ? 'factory' : 'page'} />
    </ShellFrame>
  );
}
