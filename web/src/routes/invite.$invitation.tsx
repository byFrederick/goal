import { createFileRoute, redirect } from '@tanstack/react-router';
import { authConfigQuery, ensureLoaded } from '../api/queries';
import { pageTitle } from '../app/route-focus';
import { SignInToSeeInvitation } from '../features/sign-in/invitation-signed-out';

// The link of an invitation email (<GOAL_PUBLIC_URL>/invite/<id>; canvas "Invite-Accept"). Signed
// out, it asks to sign in and comes back here. The invitation itself (join, decline, the wrong
// account) is Members' (spec 3, PR 3-11); until then a signed-in person opens the app. Sign-in off:
// there are no invitations.
export const Route = createFileRoute('/invite/$invitation')({
  beforeLoad: async ({ context }) => {
    const config = await ensureLoaded(context.queryClient, authConfigQuery);
    if (config.mode === 'none') throw redirect({ to: '/', replace: true });
    const { hasSession } = await import('../api/auth');
    if (await hasSession()) throw redirect({ to: '/', replace: true });
  },
  head: () => pageTitle('Invitation'),
  component: function Invitation() {
    const { invitation } = Route.useParams();
    return <SignInToSeeInvitation path={`/invite/${encodeURIComponent(invitation)}`} />;
  },
});
