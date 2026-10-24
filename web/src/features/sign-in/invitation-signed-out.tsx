// Invite-Accept, signed out (canvas "Invite-Accept", "Mobile-Invite-Accept"): sign in first, then
// the invitation opens.
import { Link } from '@tanstack/react-router';
import { AuthHeading, AuthPage } from './auth-page';

export function SignInToSeeInvitation({ path }: { path: string }) {
  return (
    <AuthPage>
      <AuthHeading>Sign in to see your invitation</AuthHeading>
      <p className="m-0 text-[15px] leading-normal text-ink-soft">
        Sign in with the email address the invitation was sent to.
      </p>
      <Link
        to="/sign-in"
        search={{ next: path }}
        className="inline-flex h-12 w-full items-center justify-center rounded-control bg-ink px-4 text-[15px] font-medium text-white no-underline transition-colors duration-200 hover:bg-ink/90"
      >
        Sign in
      </Link>
    </AuthPage>
  );
}
