// The re-auth dialog itself (reauth.tsx opens it). Two steps: how to confirm (an emailed code, or
// Google, each when the install has it), then the code, entered here so the page underneath keeps
// its state.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouterState } from '@tanstack/react-router';
import { type FormEvent, useState } from 'react';
import { authMutations } from '../api/auth';
import { authConfigQuery, keys, meQuery } from '../api/queries';
import { Button } from '../ui/button';
import { Callout } from '../ui/callout';
import { CodeField } from '../ui/code-field';
import { Dialog, DialogContent } from '../ui/dialog';
import { Field } from '../ui/field';
import { GoogleButton } from '../ui/google-button';
import { Spinner } from '../ui/spinner';
import { sendFailureText, useCodeSignIn } from './code-sign-in';
import { reauthReturnKey } from './reauth';
import { ResendLink } from './resend-link';

export default function ReauthDialog({ onDone }: { onDone: (signedIn: boolean) => void }) {
  const me = useQuery(meQuery);
  const config = useQuery(authConfigQuery);
  const email = me.data?.user?.email ?? '';
  const [step, setStep] = useState<'ask' | 'code'>('ask');

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onDone(false))}>
      <DialogContent title="Confirm it’s you">
        {step === 'ask' ? (
          <AskStep
            email={email}
            google={config.data?.methods.includes('google') ?? false}
            codes={config.data?.methods.includes('email_code') ?? true}
            onSent={() => setStep('code')}
            onCancel={() => onDone(false)}
          />
        ) : (
          <CodeStep email={email} onSignedIn={() => onDone(true)} onCancel={() => onDone(false)} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function AskStep({
  email,
  google,
  codes,
  onSent,
  onCancel,
}: {
  email: string;
  google: boolean;
  /** Whether the install sends codes; a Google-only one (GOAL_AUTH_EMAIL=off) does not. */
  codes: boolean;
  onSent: () => void;
  onCancel: () => void;
}) {
  const href = useRouterState({ select: (s) => s.location.href });
  const send = useMutation({ ...authMutations.sendCode, onSuccess: onSent });
  const withGoogle = useMutation(authMutations.signInWithGoogle);

  const continueWithGoogle = () => {
    try {
      sessionStorage.setItem(reauthReturnKey, '1');
    } catch {
      // Storage refused: back from Google the page only shows no toast.
    }
    withGoogle.mutate({ callbackURL: href, errorCallbackURL: `/sign-in?next=${encodeURIComponent(href)}` });
  };

  return (
    <div className="flex flex-col gap-4">
      <p className="m-0 text-[15px] leading-normal text-ink-soft">
        This change needs a recent sign-in.{' '}
        {codes ? `We’ll email a code to ${email || 'your address'}.` : 'Sign in with Google again.'}
      </p>
      <div aria-live="polite" className="empty:-mt-4">
        {send.isError ? <Callout tone="rework">{sendFailureText(send.error)}</Callout> : null}
        {withGoogle.isError ? <Callout tone="rework">Google sign-in didn’t finish. Try again.</Callout> : null}
      </div>
      <div className="flex flex-col gap-2">
        {codes ? (
          <Button
            variant="primary"
            size="lg"
            aria-busy={send.isPending || undefined}
            onClick={() => {
              if (!send.isPending && email) send.mutate(email);
            }}
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
        ) : null}
        {google ? <GoogleButton pending={withGoogle.isPending} onClick={continueWithGoogle} /> : null}
        <Button variant="secondary" size="lg" onClick={onCancel} className="h-12 w-full justify-center">
          Cancel
        </Button>
      </div>
    </div>
  );
}

function CodeStep({ email, onSignedIn, onCancel }: { email: string; onSignedIn: () => void; onCancel: () => void }) {
  const queryClient = useQueryClient();
  const signIn = useCodeSignIn(email, async () => {
    // The new session's times: what the account menu and later checks read.
    await queryClient.invalidateQueries({ queryKey: keys.me });
    onSignedIn();
  });
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    // React bubbles submit through portals: the form under the dialog must not see it.
    e.stopPropagation();
    signIn.submit();
  };
  const message = signIn.message;

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-3.5">
      <p className="m-0 text-[15px] leading-normal text-ink-soft">We sent a 6-digit code to {email}.</p>
      <Field label="Code">
        <CodeField
          value={signIn.code}
          onChange={signIn.type}
          look={signIn.look}
          aria-describedby={message?.tone === 'rework' ? 'reauth-message' : undefined}
          autoFocus
        />
      </Field>
      <div aria-live="polite" className="empty:-mt-3.5">
        {message ? (
          <Callout tone={message.tone} id="reauth-message">
            {message.text}
          </Callout>
        ) : null}
      </div>
      <Button
        type="submit"
        variant="primary"
        size="lg"
        aria-busy={signIn.verifying || signIn.signedIn || undefined}
        disabled={signIn.usedUp}
        className="h-12 w-full justify-center aria-busy:cursor-progress"
      >
        {signIn.verifying || signIn.signedIn ? (
          <>
            <Spinner />
            Confirming…
          </>
        ) : (
          'Confirm and continue'
        )}
      </Button>
      <div className="flex flex-wrap justify-between gap-x-4 gap-y-2 text-sm">
        <ResendLink at={signIn.resendAt} pending={signIn.resending} onResend={signIn.resend} />
        <button
          type="button"
          onClick={onCancel}
          className="inline-flex min-h-11 items-center border-0 bg-transparent p-0 text-sm md:min-h-6 text-ink underline underline-offset-[3px] transition-colors duration-200 hover:text-run"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}
