import { createFileRoute, redirect } from '@tanstack/react-router';
import { ensureLoaded, meQuery, orgsQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { NotInAnOrganization } from '../features/sign-in/not-in-an-organization';
import { defaultOrg } from '../shared/org';
import { LogoMark } from '../ui/logo';

// The organization used last (or the first goald lists). Without any: with sign-in on, the person
// is in no organization yet (Sign-In board, "Signed in, no organization"); without it, a page
// saying how to make one.
export const Route = createFileRoute('/')({
  loader: async ({ context }) => {
    const org = defaultOrg(await ensureLoaded(context.queryClient, orgsQuery));
    if (org) throw redirect({ to: '/o/$org', params: { org: org.slug }, replace: true });
    return { me: await ensureLoaded(context.queryClient, meQuery).catch(() => undefined) };
  },
  head: () => pageTitle('No organization'),
  component: function Start() {
    const { me } = Route.useLoaderData();
    return me?.mode === 'session' && me.user ? <NotInAnOrganization email={me.user.email} /> : <NoOrganization />;
  },
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
