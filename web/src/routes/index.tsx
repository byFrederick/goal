import { createFileRoute, redirect } from '@tanstack/react-router';
import { ensureLoaded, orgsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { defaultOrg } from '../shared/org';
import { LogoMark } from '../ui/logo';

// The organization used last (or the first goald lists); a page saying how to make one when the
// install has none.
export const Route = createFileRoute('/')({
  loader: async ({ context }) => {
    const org = defaultOrg(await ensureLoaded(context.queryClient, orgsQuery));
    if (org) throw redirect({ to: '/o/$org', params: { org: org.slug }, replace: true });
  },
  head: () => pageTitle('No organization'),
  component: NoOrganization,
});

function NoOrganization() {
  return (
    <main id="main" className="mx-auto flex min-h-dvh max-w-xl flex-col justify-center gap-4 p-6">
      <LogoMark />
      <h1 className="m-0 text-4xl font-normal tracking-display">No organization yet</h1>
      <p className="m-0 text-[15px] leading-relaxed text-muted">
        Factories belong to an organization. Create one with{' '}
        <code>goal org create &lt;address&gt; --name &lt;name&gt;</code> and this page opens its factories.
      </p>
    </main>
  );
}
