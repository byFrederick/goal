import { createFileRoute, redirect } from '@tanstack/react-router';
import { authConfigQuery, ensureLoaded } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { InvitationPage } from '../features/sign-in/invitation-page';
import { SignInToSeeInvitation } from '../features/sign-in/invitation-signed-out';

// The link of an invitation email (<GOAL_PUBLIC_URL>/invite/<id>; canvas "Invite-Accept"). Signed
// out, it asks to sign in and comes back here; signed in, the invitation itself: join or decline
// (spec 3 §10.2). Sign-in off: there are no invitations.
export const Route = createFileRoute('/invite/$invitation')({
  beforeLoad: async ({ context }) => {
    const config = await ensureLoaded(context.queryClient, authConfigQuery);
    if (config.mode === 'none') throw redirect({ to: '/', replace: true });
    const { hasSession } = await import('../api/auth');
    return { signedIn: await hasSession() };
  },
  head: () => pageTitle('Invitation'),
  component: function Invitation() {
    const { invitation } = Route.useParams();
    const { signedIn } = Route.useRouteContext();
    return signedIn ? (
      <InvitationPage id={invitation} />
    ) : (
      <SignInToSeeInvitation path={`/invite/${encodeURIComponent(invitation)}`} />
    );
  },
});
