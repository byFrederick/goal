import { createFileRoute, redirect } from '@tanstack/react-router';
import { authConfigQuery, ensureLoaded, meQuery } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { DevicePage, SignInToApprove } from '../features/sign-in/device-page';

// Where goal login sends the person (<GOAL_PUBLIC_URL>/device; canvas "Device-Approve"). It has no
// search parameters on purpose: a code in the URL (verification_uri_complete) is never read, so a
// link someone sends cannot pre-fill or approve anything. Signed out, it asks to sign in and comes
// back here. Sign-in off: there is no CLI sign-in.
export const Route = createFileRoute('/device')({
  beforeLoad: async ({ context }) => {
    const config = await ensureLoaded(context.queryClient, authConfigQuery);
    if (config.mode === 'none') throw redirect({ to: '/', replace: true });
  },
  loader: async ({ context }) => {
    const { hasSession } = await import('../api/auth');
    if (!(await hasSession())) return { signedIn: false };
    // Who approves, for "Goal CLI wants to sign in as …".
    await ensureLoaded(context.queryClient, meQuery);
    return { signedIn: true };
  },
  head: () => pageTitle('Sign in to the Goal CLI'),
  component: function Device() {
    const { signedIn } = Route.useLoaderData();
    return signedIn ? <DevicePage /> : <SignInToApprove />;
  },
});
