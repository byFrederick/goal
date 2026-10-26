// Signed in, in no organization (canvas "Sign-In", state "Signed in, no organization"): every
// invitation was used or cancelled. Only an owner can help; the person can sign out.
import { LogOut } from 'lucide-react';
import { useSignOut } from '../../shared/account';
import { Button } from '../../ui/button';
import { Spinner } from '../../ui/spinner';
import { AuthHeading, AuthPage } from './auth-page';

export function NotInAnOrganization({ email }: { email: string }) {
  const { signOut } = useSignOut();
  return (
    <AuthPage>
      <AuthHeading>Not in an organization yet</AuthHeading>
      <p className="m-0 text-[15px] leading-normal text-ink-soft">
        You’re signed in as {email} but not in an organization yet. Ask an owner for an invitation.
      </p>
      <Button
        variant="white"
        size="lg"
        aria-busy={signOut.isPending || undefined}
        onClick={() => {
          if (!signOut.isPending) signOut.mutate();
        }}
        className="h-12 w-full justify-center aria-busy:cursor-progress"
      >
        {signOut.isPending ? <Spinner tone="dark" /> : <LogOut size={16} strokeWidth={1.5} aria-hidden="true" />}
        Sign out
      </Button>
    </AuthPage>
  );
}
