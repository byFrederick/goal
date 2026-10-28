// Sign in (canvas "Sign-In", "Mobile-Sign-In"; spec 3 §10.2): an email for a code, or Google when
// the install turns it on. Goal is invite-only: the identity service answers the same for every
// address and sends a code only to people who may sign in, so this page never says whether an
// address is known.
import { useMutation, useSuspenseQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { type FormEvent, useState } from 'react';
import { AuthFailure, authMutations, googleFailure } from '../../api/auth';
import { authConfigQuery } from '../../api/queries';
import { rateLimitText, sendFailureText } from '../../shared/code-sign-in';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { useSecondsLeft } from '../../ui/countdown';
import { Field, Input } from '../../ui/field';
import { GoogleButton } from '../../ui/google-button';
import { NotAvailable } from '../../ui/not-available';
import { Spinner } from '../../ui/spinner';
import { AuthHeading, AuthPage, LiveMessage } from './auth-page';

const emailRule = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** Back from Google without signing in. */
const googleText = {
  'not-invited': 'That Google account isn’t invited to Goal.',
  unverified: 'Google didn’t share a verified email for that account. Sign in with an email code instead.',
  unverifiedOnly: 'Google didn’t share a verified email for that account. Try another Google account.',
  failed: 'Google sign-in didn’t finish. Try again.',
} as const;

/** The GitHub mark, drawn here: the icon set has none. Decorative: the button's label says it. */
function GitHubGlyph() {
  return (
    <svg viewBox="0 0 24 24" width={18} height={18} aria-hidden="true" focusable="false" className="fill-current">
      <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
    </svg>
  );
}

interface SignInPageProps {
  /** The page to open once signed in (already checked: a path of this origin). */
  next: string | undefined;
  /** Google's answer when it sent the person back here (?error=). */
  googleError: string | undefined;
}

export function SignInPage({ next, googleError }: SignInPageProps) {
  const { data: config } = useSuspenseQuery(authConfigQuery);
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [invalid, setInvalid] = useState<string>();
  // A rate limit keeps the button off until the wait is over, and says how long is left.
  const [blockedUntil, setBlockedUntil] = useState<number | null>(null);
  const waitLeft = useSecondsLeft(blockedUntil);
  const blocked = waitLeft > 0;

  const send = useMutation({
    ...authMutations.sendCode,
    onSuccess: (_, address) =>
      navigate({ to: '/sign-in/code', search: next ? { next } : {}, state: { email: address } }),
    onError: (err) => {
      if (err instanceof AuthFailure && err.kind === 'rate-limited') {
        setBlockedUntil(Date.now() + (err.retryAfterSeconds ?? 60) * 1000);
      }
    },
  });
  const google = useMutation(authMutations.signInWithGoogle);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const address = email.trim();
    if (!emailRule.test(address)) {
      setInvalid('Enter your email address, like you@example.com.');
      return;
    }
    setInvalid(undefined);
    if (!send.isPending) send.mutate(address);
  };

  const continueWithGoogle = () => {
    const back = next ?? '/';
    google.mutate({ callbackURL: back, errorCallbackURL: `/sign-in?next=${encodeURIComponent(back)}` });
  };

  const rateLimited = send.error instanceof AuthFailure && send.error.kind === 'rate-limited';
  const sendError = blocked
    ? rateLimitText(waitLeft)
    : send.isError && !rateLimited
      ? sendFailureText(send.error)
      : undefined;
  const withGoogle = config.methods.includes('google');
  // A Google-only install (GOAL_AUTH_EMAIL=off) has no mail server: no code to ask for.
  const withCodes = config.methods.includes('email_code');
  const googleFailed = googleError ? googleFailure(googleError) : undefined;
  const googleProblem = google.isError
    ? googleText.failed
    : googleFailed === 'unverified' && !withCodes
      ? googleText.unverifiedOnly
      : googleFailed
        ? googleText[googleFailed]
        : undefined;

  return (
    <AuthPage band>
      {googleProblem ? <Callout tone="rework">{googleProblem}</Callout> : null}
      <div className="flex flex-col gap-2">
        <AuthHeading>Sign in to Goal</AuthHeading>
        <p className="m-0 text-body text-ink-soft">Your factories, and what needs you, wherever you are.</p>
      </div>
      {/* GitHub sign-in is not built yet (ADR 0016, decision 4): the button is there, disabled. */}
      <NotAvailable label="Continue with GitHub">
        <Button variant="primary" size="lg" className="w-full justify-center">
          <GitHubGlyph />
          Continue with GitHub
        </Button>
      </NotAvailable>
      {withGoogle ? <GoogleButton pending={google.isPending} onClick={continueWithGoogle} /> : null}
      {withCodes ? (
        <>
          <div
            aria-hidden="true"
            className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-3 text-secondary text-muted before:h-px before:bg-rule after:h-px after:bg-rule"
          >
            or
          </div>
          <form onSubmit={onSubmit} noValidate className="flex flex-col gap-3">
            <Field label="Work email" error={invalid}>
              <Input
                type="email"
                name="email"
                autoComplete="email"
                placeholder="you@example.com"
                spellCheck={false}
                autoCapitalize="off"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </Field>
            <Button
              type="submit"
              variant="white"
              size="lg"
              aria-busy={send.isPending || undefined}
              disabled={blocked}
              className="w-full justify-center aria-busy:cursor-progress"
            >
              {send.isPending ? (
                <>
                  <Spinner tone="dark" />
                  Sending code…
                </>
              ) : (
                'Email me a sign-in code'
              )}
            </Button>
            <LiveMessage>{sendError ? <Callout tone="rework">{sendError}</Callout> : null}</LiveMessage>
          </form>
        </>
      ) : null}
      <Callout tone="note">Goal is invite-only. Ask an owner of your organization to invite you.</Callout>
    </AuthPage>
  );
}
