// The one module that calls the identity service, /api/auth/* on goald's origin (spec 3 §8.1,
// ADR 0007): Better Auth's own browser client and contract, at the version auth/ pins (the pnpm
// catalog), with the plugins of the paths goald's proxy allows (email codes, organizations,
// device sign-in). It is loaded only where someone signs in or out (the sign-in pages, the re-auth
// dialog, the account menu's actions, the keep-alive), never in the app's first bundle.
//
// goald's /v1 never sees these: it reads the session from its cookie, which no script can read
// (httpOnly), so nothing here holds a token.
import { mutationOptions } from '@tanstack/react-query';
import { createAuthClient } from 'better-auth/client';
import { deviceAuthorizationClient, emailOTPClient, organizationClient } from 'better-auth/client/plugins';
import { sessionHandlers } from './session';

const auth = createAuthClient({
  baseURL: window.location.origin,
  basePath: '/api/auth',
  plugins: [emailOTPClient(), organizationClient(), deviceAuthorizationClient()],
});

/**
 * Why a call to the identity service failed, in the words the screens use: it could not be
 * reached (network, 5xx, goald's 503 auth-unavailable); too many tries from this network (429,
 * with the seconds to wait); a wrong code (also when sign-in by code is locked for the address,
 * which the service answers the same way); an expired code; a code used up after 3 tries; an
 * address that may not sign up (no invitation); a change that needed a recent sign-in the person
 * did not confirm; a name that is not just a name; or anything else the service refused.
 */
export type AuthFailureKind =
  | 'unreachable'
  | 'rate-limited'
  | 'wrong-code'
  | 'expired'
  | 'used-up'
  | 'not-invited'
  | 'reauth-required'
  | 'name-refused'
  | 'refused';

export class AuthFailure extends Error {
  readonly kind: AuthFailureKind;
  /** rate-limited only: how long to wait, from the X-Retry-After header. */
  readonly retryAfterSeconds: number | undefined;

  constructor(kind: AuthFailureKind, retryAfterSeconds?: number) {
    super(`identity service: ${kind}`);
    this.name = 'AuthFailure';
    this.kind = kind;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

/**
 * The service's error codes: Better Auth's of the email-code sign-in (better-auth/plugins/email-otp;
 * TOO_MANY_ATTEMPTS is the 4th try of one code) and Goal's hooks' (auth/src: SIGN_UP_NOT_ALLOWED,
 * REAUTH_REQUIRED, NAME_ONLY).
 */
const codeFailures: Record<string, AuthFailureKind> = {
  INVALID_OTP: 'wrong-code',
  OTP_EXPIRED: 'expired',
  TOO_MANY_ATTEMPTS: 'used-up',
  SIGN_UP_NOT_ALLOWED: 'not-invited',
  REAUTH_REQUIRED: 'reauth-required',
  NAME_ONLY: 'name-refused',
};

interface BetterAuthError {
  status: number;
  code?: string | undefined;
}

type BetterAuthResult<T> = { data: T; error: null } | { data: null; error: BetterAuthError };

type Run<T> = (fetchOptions: { onError: (ctx: { response: Response }) => void }) => Promise<BetterAuthResult<T>>;

/**
 * Runs one call, turning a failure into an AuthFailure. A change the service refuses with
 * REAUTH_REQUIRED (a session older than 10 minutes) asks the person to confirm it's them, as goald's
 * 403 reauth-required does, and is sent once more.
 */
async function call<T>(run: Run<T>, retried = false): Promise<T> {
  let retryAfter: string | null = null;
  let result: BetterAuthResult<T>;
  try {
    result = await run({ onError: ({ response }) => (retryAfter = response.headers.get('X-Retry-After')) });
  } catch {
    throw new AuthFailure('unreachable');
  }
  if (!result.error) return result.data;
  const { status, code } = result.error;
  if (status === 429) throw new AuthFailure('rate-limited', Number(retryAfter) || 60);
  if (status >= 500 || status === 0) throw new AuthFailure('unreachable');
  const kind = (code && codeFailures[code]) || 'refused';
  if (kind === 'reauth-required' && !retried && (await sessionHandlers.reauth())) return call(run, true);
  throw new AuthFailure(kind);
}

/**
 * Asks for a sign-in code for the address. The service answers the same for every address and
 * sends the code only to people who may sign in (spec 3 §8.7).
 */
export async function sendCode(email: string): Promise<void> {
  await call((fetchOptions) => auth.emailOtp.sendVerificationOtp({ email, type: 'sign-in', fetchOptions }));
}

/** Signs in with the 6-digit code: the service sets the session cookie. */
export async function signInWithCode(email: string, otp: string): Promise<void> {
  await call((fetchOptions) => auth.signIn.emailOtp({ email, otp, fetchOptions }));
}

/**
 * Leaves for Google. Back from it, the browser opens `callbackURL` signed in, or
 * `errorCallbackURL` with ?error=<code> (googleFailure reads it).
 */
export async function signInWithGoogle(callbackURL: string, errorCallbackURL: string): Promise<void> {
  await call((fetchOptions) => auth.signIn.social({ provider: 'google', callbackURL, errorCallbackURL, fetchOptions }));
}

/**
 * Better Auth extends a session only when its own endpoints see it (spec 3 §8.3), so the app asks
 * for the session on load and hourly. Failures are ignored: the next /v1 request says whether the
 * session is still good.
 */
export async function keepSessionAlive(): Promise<void> {
  await auth.getSession().catch(() => undefined);
}

/** Whether this browser is signed in, by the identity service's own answer (no /v1 401). */
export async function hasSession(): Promise<boolean> {
  const result = await auth.getSession().catch(() => null);
  return Boolean(result?.data);
}

/** Ends this browser's session. */
export async function signOut(): Promise<void> {
  await call((fetchOptions) => auth.signOut({ fetchOptions }));
}

/** Ends every session of the person: every browser and the CLI. API keys keep working. */
export async function signOutEverywhere(): Promise<void> {
  await call((fetchOptions) => auth.revokeSessions({ fetchOptions }));
}

/** Changes the person's name, the only account field Goal changes (1 to 80 characters). */
export async function updateName(name: string): Promise<void> {
  await call((fetchOptions) => auth.updateUser({ name, fetchOptions }));
}

/**
 * What an ?error= from Google's return means: the account may not sign up (Goal's sign-up gate,
 * SIGN_UP_NOT_ALLOWED; Better Auth's own unable_to_create_user or signup_disabled); Google did not
 * give a verified email (unable_to_get_user_info); or anything else (the person closed Google's
 * window, Google refused).
 */
export function googleFailure(error: string): 'not-invited' | 'unverified' | 'failed' {
  if (['SIGN_UP_NOT_ALLOWED', 'unable_to_create_user', 'signup_disabled'].includes(error)) return 'not-invited';
  return error === 'unable_to_get_user_info' ? 'unverified' : 'failed';
}

/** The calls as TanStack mutations. */
export const authMutations = {
  sendCode: mutationOptions({ mutationKey: ['auth', 'send-code'], mutationFn: sendCode }),
  signInWithCode: mutationOptions({
    mutationKey: ['auth', 'sign-in', 'code'],
    mutationFn: ({ email, code }: { email: string; code: string }) => signInWithCode(email, code),
  }),
  signInWithGoogle: mutationOptions({
    mutationKey: ['auth', 'sign-in', 'google'],
    mutationFn: ({ callbackURL, errorCallbackURL }: { callbackURL: string; errorCallbackURL: string }) =>
      signInWithGoogle(callbackURL, errorCallbackURL),
  }),
  signOut: mutationOptions({ mutationKey: ['auth', 'sign-out'], mutationFn: signOut }),
  signOutEverywhere: mutationOptions({
    mutationKey: ['auth', 'sign-out', 'everywhere'],
    mutationFn: signOutEverywhere,
  }),
  updateName: mutationOptions({ mutationKey: ['auth', 'update-name'], mutationFn: updateName }),
};
