// Goal's sign-in rules on top of Better Auth (spec 3 §8.7): who may get a user, who gets a code,
// the send cap, the lockout and its notice, the bootstrap owner, and the security log entries
// (auth_events) for each. Codes are sent in the background, so the send endpoint answers the same
// thing in the same time for anyone; nothing here logs an email address or a code.
import type { BetterAuthOptions } from 'better-auth';
import { APIError } from 'better-auth/api';
import { type AuthEvent, type Clock, emailSubject, type GoalDatabase } from './goal.js';
import type { Logger } from './log.js';
import type { Mail, Mailer } from './mail/mailer.js';
import { lockoutMail, signInCodeMail } from './mail/messages.js';

type DatabaseHooks = NonNullable<BetterAuthOptions['databaseHooks']>;
type UserCreate = NonNullable<NonNullable<DatabaseHooks['user']>['create']>;
type SessionCreate = NonNullable<NonNullable<DatabaseHooks['session']>['create']>;

/** EndpointContext is Better Auth's context of the request a hook runs in (null outside one). */
export type EndpointContext = Parameters<NonNullable<UserCreate['before']>>[1];

/** The limits of §8.7: codes per address per hour, failed codes per address per day. */
export const limits = { codesPerHour: 5, failuresPerDay: 10 } as const;

const hour = 3600_000;
const day = 24 * hour;

/** The answer to a wrong code, which a locked address gets too (spec 3 §8.7). */
export function invalidCode(): APIError {
  // Key for key as Better Auth's own answer (message first), so the body is byte for byte the same.
  return new APIError('BAD_REQUEST', { message: 'Invalid OTP', code: 'INVALID_OTP' });
}

/**
 * deliver sends m and reports whether it went. A failure is logged by its error code only: SMTP
 * errors can repeat the recipient's address, which never reaches the logs.
 */
export async function deliver(mailer: Mailer, log: Logger, m: Mail, fields: Record<string, unknown>): Promise<boolean> {
  try {
    await mailer.send(m);
    return true;
  } catch (e) {
    const code = typeof e === 'object' && e !== null && 'code' in e ? String(e.code) : 'unknown';
    log.log('error', 'mail.failed', { ...fields, code });
    return false;
  }
}

/** client is the request's address (written by goald's proxy) and user agent, for the log. */
export function client(ctx: EndpointContext | undefined): Pick<AuthEvent, 'ip' | 'userAgent'> {
  const headers = ctx?.headers ?? ctx?.request?.headers;
  return { ip: headers?.get('x-goal-client-ip'), userAgent: headers?.get('user-agent') };
}

/** SignInMethod is how a session or user came to be (auth.session."signInMethod", 00048's CHECK). */
export type SignInMethod = 'email_code' | 'google' | 'device' | 'other';

/**
 * browserSignIn is whether a session was signed in through the browser, by an email code or Google, as
 * recorded when it was created (review M1): goal login's device flow, other and an untagged session
 * are not, however their token is sent. goald reads the same column (domain.Session.Browser).
 */
export function browserSignIn(session: object): boolean {
  const m = (session as { signInMethod?: unknown }).signInMethod;
  return m === 'email_code' || m === 'google';
}

/** method names how a session or user came to be, from the endpoint that made it. */
function method(ctx: EndpointContext | undefined): SignInMethod {
  switch (ctx?.path) {
    case '/sign-in/email-otp':
      return 'email_code';
    case '/callback/:id':
    case '/sign-in/social':
      return 'google';
    case '/device/token':
      return 'device';
    default:
      return 'other';
  }
}

export interface SignInDeps {
  db: GoalDatabase;
  /** Mails codes and the lockout notice; none while email codes are off (GOAL_AUTH_EMAIL=off). */
  mailer: Mailer | undefined;
  log: Logger;
  clock: Clock;
  /** GOAL_BOOTSTRAP_OWNER_EMAIL, lower case. */
  bootstrapOwnerEmail: string | undefined;
}

export class SignIn {
  constructor(private readonly deps: SignInDeps) {}

  /**
   * allowOnlyInvitedOrBootstrap is the single sign-up gate, for codes and Google alike: a user is
   * created only for a verified email with a pending, unexpired invitation, or for the bootstrap
   * owner's email while no user exists. Anyone else is refused and logged.
   */
  allowOnlyInvitedOrBootstrap: NonNullable<UserCreate['before']> = async (user, ctx) => {
    const { db, clock, bootstrapOwnerEmail } = this.deps;
    const email = user.email.trim().toLowerCase();
    if (user.emailVerified === true) {
      if (await db.isInvitee(email, clock())) return;
      if (email === bootstrapOwnerEmail && !(await db.hasUsers())) return;
    }
    await db.record({
      kind: 'sign_up.refused',
      subject: emailSubject(email),
      ...client(ctx ?? undefined),
      data: { method: method(ctx ?? undefined) },
    });
    throw new APIError('FORBIDDEN', { code: 'SIGN_UP_NOT_ALLOWED', message: 'Only invited people can sign up' });
  };

  recordUser: NonNullable<UserCreate['after']> = async (user, ctx) => {
    await this.deps.db.record({
      kind: 'user.created',
      user: user.id,
      ...client(ctx ?? undefined),
      data: { method: method(ctx ?? undefined) },
    });
  };

  /**
   * sendCodeIfAllowed is email OTP's sender, run in the background: it mails a sign-in code only
   * to a user or a pending invitee, at most codesPerHour an hour, and never to a locked address.
   * Otherwise it records why (otp.suppressed) and voids the code Better Auth stored, so the only
   * usable codes are ones that were sent.
   */
  sendCodeIfAllowed = async (
    { email, otp, type }: { email: string; otp: string; type: string },
    ctx?: EndpointContext,
  ): Promise<void> => {
    const { db, mailer, log, clock } = this.deps;
    const subject = emailSubject(email);
    const from = client(ctx);
    const now = clock().getTime();

    let reason: 'unknown' | 'locked' | 'cap' | undefined;
    if (type !== 'sign-in' || !(await this.known(email))) reason = 'unknown';
    else if (await this.locked(subject)) reason = 'locked';
    else if ((await db.count('otp.sent', subject, new Date(now - hour))) >= limits.codesPerHour) reason = 'cap';

    if (reason) {
      await ctx?.context.internalAdapter.deleteVerificationByIdentifier(`${type}-otp-${email}`);
      await db.record({ kind: 'otp.suppressed', subject, ...from, data: { reason } });
      log.log('info', 'otp.suppressed', { subject, reason });
      return;
    }
    if (!mailer || !(await deliver(mailer, log, signInCodeMail(email, otp), { mail: 'sign_in_code', subject }))) return;
    await db.record({ kind: 'otp.sent', subject, ...from });
    log.log('info', 'otp.sent', { subject });
  };

  /** refuseWhileLocked answers a locked address's code like a wrong one, without trying it. */
  async refuseWhileLocked(email: string): Promise<void> {
    if (await this.locked(emailSubject(email))) throw invalidCode();
  }

  /**
   * recordFailure logs a failed code. The failure that locks the address logs otp.locked and, for a
   * user or invitee, mails them once, in the background so the answer takes as long as any other.
   */
  async recordFailure(email: string, ctx: EndpointContext | undefined): Promise<void> {
    const { db, mailer, log, clock } = this.deps;
    const subject = emailSubject(email);
    const from = client(ctx);
    await db.record({ kind: 'otp.failed', subject, ...from });

    const since = new Date(clock().getTime() - day);
    if ((await db.count('otp.failed', subject, since)) < limits.failuresPerDay) return;
    if ((await db.count('otp.locked', subject, since)) > 0) return;
    await db.record({ kind: 'otp.locked', subject, ...from });
    log.log('warn', 'otp.locked', { subject });

    const notify = async () => {
      if (mailer && (await this.known(email)))
        await deliver(mailer, log, lockoutMail(email), { mail: 'lockout', subject });
    };
    if (ctx) await ctx.context.runInBackgroundOrAwait(notify());
    else await notify();
  }

  /**
   * tagSignInMethod records on every new session how the person signed in (signInMethod), so goald
   * and the before-hook tell the browser from goal login by the session, never by how its token
   * arrives (review M1).
   */
  tagSignInMethod: NonNullable<SessionCreate['before']> = async (session, ctx) => ({
    data: { ...session, signInMethod: method(ctx ?? undefined) },
  });

  /**
   * recordSignInAndClaimBootstrap logs every new session and, for the bootstrap owner's email,
   * claims the bootstrap (goal_claim_bootstrap) until it holds, so a failed first attempt heals on
   * the next sign-in. Its failures are logged; they never undo the sign-in.
   */
  recordSignInAndClaimBootstrap: NonNullable<SessionCreate['after']> = async (session, ctx) => {
    const { db, log, bootstrapOwnerEmail } = this.deps;
    try {
      await db.record({
        kind: 'sign_in.succeeded',
        user: session.userId,
        ...client(ctx ?? undefined),
        data: { method: method(ctx ?? undefined) },
      });
      if (bootstrapOwnerEmail === undefined) return;
      const user = await ctx?.context.internalAdapter.findUserById(session.userId);
      if (user?.email.toLowerCase() !== bootstrapOwnerEmail) return;
      if (await db.claimBootstrap(session.userId)) log.log('info', 'bootstrap.claimed', { user_id: session.userId });
      else log.log('warn', 'bootstrap.not_claimed', { user_id: session.userId });
    } catch (e) {
      log.log('error', 'sign_in.record_failed', { error: e });
    }
  };

  /** known: a user, a pending invitee, or the bootstrap owner while no user exists. */
  private async known(email: string): Promise<boolean> {
    const { db, clock, bootstrapOwnerEmail } = this.deps;
    if ((await db.isUser(email)) || (await db.isInvitee(email, clock()))) return true;
    return email.trim().toLowerCase() === bootstrapOwnerEmail && !(await db.hasUsers());
  }

  private async locked(subject: string): Promise<boolean> {
    const since = new Date(this.deps.clock().getTime() - day);
    return (await this.deps.db.count('otp.failed', subject, since)) >= limits.failuresPerDay;
  }
}
