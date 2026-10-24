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
import { Spinner } from '../../ui/spinner';
import { AuthHeading, AuthPage, LiveMessage } from './auth-page';

const emailRule = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** Back from Google without signing in. */
const googleText = {
  'not-invited': 'That Google account isn’t invited to Goal.',
  unverified: 'Google didn’t share a verified email for that account. Sign in with an email code instead.',
  failed: 'Google sign-in didn’t finish. Try again.',
} as const;

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
  const googleProblem = google.isError
    ? googleText.failed
    : googleError
      ? googleText[googleFailure(googleError)]
      : undefined;
  const withGoogle = config.methods.includes('google');

  return (
    <AuthPage>
      {googleProblem ? <Callout tone="rework">{googleProblem}</Callout> : null}
      <AuthHeading>Sign in to Goal</AuthHeading>
      <form onSubmit={onSubmit} noValidate className="flex flex-col gap-3.5">
        <Field label="Email" error={invalid}>
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
          variant="primary"
          size="lg"
          aria-busy={send.isPending || undefined}
          disabled={blocked}
          className="h-12 w-full justify-center aria-busy:cursor-progress"
        >
          {send.isPending ? (
            <>
              <Spinner />
              Sending code…
            </>
          ) : (
            'Email me a code'
          )}
        </Button>
        <LiveMessage>{sendError ? <Callout tone="rework">{sendError}</Callout> : null}</LiveMessage>
      </form>
      {withGoogle ? (
        <>
          <div
            aria-hidden="true"
            className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-3 text-[13px] text-muted before:h-px before:bg-rule after:h-px after:bg-rule"
          >
            or
          </div>
          <GoogleButton pending={google.isPending} onClick={continueWithGoogle} />
        </>
      ) : null}
      <Callout tone="note">Goal is invite-only. Ask an owner of your organization to invite you.</Callout>
    </AuthPage>
  );
}
