// Signing in with an emailed code, as the Sign-In-Code page and the re-auth dialog both do (spec 3
// §10.2): the code as it is typed, sending it once six digits are in, the answers in the boards'
// words, and "Send a new code" after 30 seconds (at once when the code expired or is used up).
// Loaded only with those two (it brings the identity service's client).
import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { AuthFailure, authMutations } from '../api/auth';
import { codeLength } from '../ui/code-field';

/** How long before another code may be asked for. */
export const resendAfterMs = 30_000;

/** Codes allow 3 tries (spec 3 §8.7); after the third wrong one the code is void. */
const allowedTries = 3;

export interface SignInMessage {
  tone: 'rework' | 'ok';
  text: string;
}

const unreachable = 'Can’t reach Goal. Check your connection and try again.';

/** "Too many tries. Try again in 2 minutes." */
export function rateLimitText(seconds: number | undefined): string {
  const minutes = Math.max(1, Math.ceil((seconds ?? 60) / 60));
  return `Too many tries. Try again in ${minutes} ${minutes === 1 ? 'minute' : 'minutes'}.`;
}

/** What a failed request for a code says. */
export function sendFailureText(err: unknown): string {
  if (err instanceof AuthFailure && err.kind === 'rate-limited') return rateLimitText(err.retryAfterSeconds);
  if (err instanceof AuthFailure && err.kind === 'refused') {
    return 'Goal couldn’t send a code to that address. Check it and try again.';
  }
  return unreachable;
}

type CodeState = 'typing' | 'wrong' | 'expired' | 'used-up';

export function useCodeSignIn(email: string, onSignedIn: () => unknown) {
  const [code, setCode] = useState('');
  const [state, setState] = useState<CodeState>('typing');
  const [wrongTries, setWrongTries] = useState(0);
  const [message, setMessage] = useState<SignInMessage | null>(null);
  const [resendAt, setResendAt] = useState(() => Date.now() + resendAfterMs);

  const verify = useMutation({
    ...authMutations.signInWithCode,
    onSuccess: () => onSignedIn(),
    onError: (err) => {
      // A wrong code, a locked address and an address that may not sign up all read as a wrong
      // code: the answer reveals nothing about the address.
      const raw = err instanceof AuthFailure ? err.kind : 'unreachable';
      const kind = raw === 'not-invited' || raw === 'refused' ? 'wrong-code' : raw;
      const tries = wrongTries + 1;
      if (kind === 'used-up' || (kind === 'wrong-code' && tries >= allowedTries)) {
        setState('used-up');
        setCode('');
        setMessage({ tone: 'rework', text: 'Send a new code to try again.' });
      } else if (kind === 'wrong-code') {
        setWrongTries(tries);
        setState('wrong');
        setMessage({ tone: 'rework', text: 'That code didn’t work. Check it, or send a new one.' });
      } else if (kind === 'expired') {
        setState('expired');
        setMessage({ tone: 'rework', text: 'That code has expired. Send a new one.' });
      } else if (kind === 'rate-limited') {
        setMessage({
          tone: 'rework',
          text: rateLimitText(err instanceof AuthFailure ? err.retryAfterSeconds : undefined),
        });
      } else {
        setMessage({ tone: 'rework', text: unreachable });
      }
    },
  });

  const resend = useMutation({
    ...authMutations.sendCode,
    onSuccess: () => {
      setCode('');
      setState('typing');
      setWrongTries(0);
      setResendAt(Date.now() + resendAfterMs);
      setMessage({ tone: 'ok', text: 'We sent a new code.' });
    },
    onError: (err) => setMessage({ tone: 'rework', text: sendFailureText(err) }),
  });

  const submit = (digits = code) => {
    if (digits.length !== codeLength || verify.isPending || verify.isSuccess || state === 'used-up') return;
    verify.mutate({ email, code: digits });
  };

  return {
    code,
    /** Takes what was typed; the sixth digit sends the code. */
    type(digits: string) {
      setCode(digits);
      // A new try: the refusal of the last one no longer describes the field.
      if (state === 'wrong' || state === 'expired') {
        setState('typing');
        setMessage(null);
      }
      if (digits.length === codeLength && digits !== code) submit(digits);
    },
    submit: () => submit(),
    look:
      state === 'used-up'
        ? ('off' as const)
        : state === 'wrong' || state === 'expired'
          ? ('error' as const)
          : ('normal' as const),
    usedUp: state === 'used-up',
    message,
    verifying: verify.isPending,
    signedIn: verify.isSuccess,
    /** When "Send a new code" enables (a Date.now() instant); null: now. */
    resendAt: state === 'expired' || state === 'used-up' ? null : resendAt,
    resend: () => resend.mutate(email),
    resending: resend.isPending,
  };
}
