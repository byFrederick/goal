// The emailed code (canvas "Sign-In-Code", "Mobile-Sign-In-Code"; spec 3 §10.2). The address
// travels in the page's history state, never in the URL. Once signed in, Goal opens the page that
// asked for sign-in, or the organization used last.
import { useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { type FormEvent, useState } from 'react';
import { keys, orgsQuery } from '../../api/queries';
import { type SignInMessage, useCodeSignIn } from '../../shared/code-sign-in';
import { defaultOrg, findOrg } from '../../shared/org';
import { ResendLink } from '../../shared/resend-link';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { CodeField } from '../../ui/code-field';
import { Field } from '../../ui/field';
import { Spinner } from '../../ui/spinner';
import { AuthHeading, AuthPage, LiveMessage } from './auth-page';

export function CodePage({ email, next }: { email: string; next: string | undefined }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [opening, setOpening] = useState<string>();

  const signIn = useCodeSignIn(email, async () => {
    // Whatever was read before belongs to nobody now: read who this is and their organizations.
    queryClient.removeQueries({ queryKey: keys.me });
    queryClient.removeQueries({ queryKey: keys.orgs });
    const orgs = await queryClient.fetchQuery(orgsQuery).catch(() => []);
    const to = next ?? (defaultOrg(orgs) ? `/o/${defaultOrg(orgs)?.slug}` : '/');
    const slug = to.match(/^\/o\/([^/?#]+)/)?.[1];
    setOpening((slug ? findOrg(orgs, decodeURIComponent(slug))?.name : undefined) ?? 'Goal');
    await navigate({ href: to, replace: true });
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    signIn.submit();
  };

  if (signIn.signedIn) {
    return (
      <AuthPage>
        <LiveMessage>
          <Callout tone="ok">Signed in. Opening {opening ?? 'Goal'}…</Callout>
        </LiveMessage>
        <p className="m-0 flex items-center gap-2.5 text-sm text-ink-soft">
          <Spinner tone="dark" />
          Loading your factories
        </p>
      </AuthPage>
    );
  }

  const message: SignInMessage | null = signIn.message;
  return (
    <AuthPage>
      <AuthHeading>Check your email</AuthHeading>
      <p className="m-0 text-[15px] leading-normal text-ink-soft">
        We sent a 6-digit code to <strong className="font-medium text-ink">{email}</strong>. It expires in 5 minutes.
      </p>
      <form onSubmit={onSubmit} noValidate className="flex flex-col gap-3.5">
        <Field label="Code">
          <CodeField
            value={signIn.code}
            onChange={signIn.type}
            look={signIn.look}
            aria-describedby={message?.tone === 'rework' ? 'code-message' : undefined}
            // The page is for the code: it takes focus as the page opens.
            autoFocus
          />
        </Field>
        <LiveMessage>
          {message ? (
            <Callout tone={message.tone} id="code-message">
              {message.text}
            </Callout>
          ) : null}
        </LiveMessage>
        <Button
          type="submit"
          variant="primary"
          size="lg"
          aria-busy={signIn.verifying || undefined}
          disabled={signIn.usedUp}
          className="h-12 w-full justify-center aria-busy:cursor-progress"
        >
          {signIn.verifying ? (
            <>
              <Spinner />
              Signing in…
            </>
          ) : (
            'Sign in'
          )}
        </Button>
      </form>
      <div className="flex flex-wrap justify-between gap-x-4 gap-y-2 text-sm">
        <ResendLink at={signIn.resendAt} pending={signIn.resending} onResend={signIn.resend} />
        <Link
          to="/sign-in"
          search={next ? { next } : {}}
          className="inline-flex min-h-11 items-center text-ink underline underline-offset-[3px] transition-colors duration-200 hover:text-run md:min-h-6"
        >
          Use a different email
        </Link>
      </div>
    </AuthPage>
  );
}
