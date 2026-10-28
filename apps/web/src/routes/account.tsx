import { createFileRoute, redirect } from '@tanstack/react-router';
import { ensureLoaded, factoriesQuery, meQuery, orgQuery, orgsQuery } from '../api/queries';
import { AppShell, ShellFrame } from '../app/app-shell';
import { pageTitle } from '../app/route-focus';
import { AccountPage } from '../features/account/account-page';
import { defaultOrg, OrgContext } from '../shared/org';
import { OrgStream } from '../shared/stream';

// The person's account (docs/design/screens/account/account.jpg). It is not under an organization's
// URL: it renders inside the shell of the organization the person last opened (defaultOrg, the one `/`
// would open), and its personal tokens are that organization's. Without an organization `/` explains
// how to make one.
export const Route = createFileRoute('/account')({
  loader: async ({ context: { queryClient } }) => {
    const org = defaultOrg(await ensureLoaded(queryClient, orgsQuery));
    if (!org) throw redirect({ to: '/', replace: true });
    await Promise.all([
      ensureLoaded(queryClient, factoriesQuery(org.id)),
      ensureLoaded(queryClient, orgQuery(org.id)),
      ensureLoaded(queryClient, meQuery).catch(() => undefined),
    ]);
    return { org };
  },
  head: () => pageTitle('Your account'),
  component: AccountRoute,
});

function AccountRoute() {
  const { org } = Route.useLoaderData();
  return (
    <OrgStream org={org.id}>
      <OrgContext value={org}>
        <AppShell org={org}>
          <ShellFrame>
            <AccountPage />
          </ShellFrame>
        </AppShell>
      </OrgContext>
    </OrgStream>
  );
}
