import { betterAuth } from 'better-auth';
import { bearer } from 'better-auth/plugins/bearer';
import { deviceAuthorization } from 'better-auth/plugins/device-authorization';
import { emailOTP } from 'better-auth/plugins/email-otp';
import { organization } from 'better-auth/plugins/organization';
import type { GoogleOptions, GoogleProfile } from 'better-auth/social-providers';
import { PostgresDialect } from 'kysely';
import type pg from 'pg';
import { ac, roles } from './access.gen.js';
import { type Clock, GoalDatabase } from './goal.js';
import { goalHooks } from './hooks.js';
import type { Logger } from './log.js';
import type { Mailer } from './mail/mailer.js';
import type { Settings } from './settings.js';
import { SignIn } from './signin.js';

/** The schema of Better Auth's tables (migrations 00040, 00041). */
export const authSchema = 'auth';

/** The one client the device authorization grant serves: `goal login`. */
export const cliClientId = 'goal-cli';

/** The endpoints whose deleted sessions are revocations (session.revoked in the security log). */
const revocations = new Set(['/sign-out', '/revoke-sessions']);

export interface AuthDeps {
  settings: Settings;
  /** The database as goal_auth (checkRole has passed). */
  pool: pg.Pool;
  log: Logger;
  /**
   * Sends sign-in codes, invitations and the lockout notice. Without one (GOAL_AUTH_EMAIL=off)
   * there is no sign-in by email code at all, Google is the only way in, and nothing is mailed.
   */
  mailer?: Mailer;
  /** The time Goal's rules use (send caps, lockout, invitation expiry, fresh sessions). */
  clock?: Clock;
}

// Fields Goal adds to Better Auth's member and invitation rows (spec 3 §6.2). changedBy holds the
// actor ref of the last change and changedAt its time, so the audit triggers name the person who
// made it; neither is accepted from a request. factoryRoles is an invitation's JSON
// [{"factoryId","role"}], applied when it is accepted.
const changedBy = { type: 'string', required: false, input: false } as const;
const changedAt = { type: 'date', required: false, input: false } as const;
const factoryRoles = { type: 'string', required: false, input: true } as const;
// How a session was signed in (migration 00048): email_code, google, device or other, written by
// the session-create hook, never accepted from a request.
const signInMethod = { type: 'string', required: false, input: false } as const;

/**
 * createAuth configures Better Auth for Goal (spec 3 §8.6): passwordless sign-in by email code or
 * Google, organizations whose lifecycle goald owns (Better Auth's create, update and delete paths
 * are off), the device grant for `goal login`, and bearer tokens for the CLI. Its tables live in
 * schema auth under goose: `auth generate` (task auth:schema-check) must find nothing to change.
 * Sign-up is invite-only (signin.ts, spec 3 §8.7) and Goal's hooks (hooks.ts) add its rules to
 * Better Auth's endpoints and write the security log.
 */
export function createAuth({ settings, pool, log, mailer, clock = () => new Date() }: AuthDeps) {
  const db = new GoalDatabase(pool);
  const signIn = new SignIn({ db, mailer, log, clock, bootstrapOwnerEmail: settings.bootstrapOwnerEmail });
  const hooks = goalHooks({ db, signIn, mailer, log, clock, publicURL: settings.publicURL });

  return betterAuth({
    appName: 'Goal',
    baseURL: settings.publicURL,
    basePath: '/api/auth',
    ...(settings.secrets ? { secrets: settings.secrets } : {}),
    ...(settings.secret ? { secret: settings.secret } : {}),
    trustedOrigins: [settings.publicURL],
    database: { dialect: new PostgresDialect({ pool }), type: 'postgres', schemaName: authSchema },
    disabledPaths: [
      '/organization/create',
      '/organization/update',
      '/organization/delete',
      '/organization/check-slug',
      '/organization/set-active',
      '/delete-user',
      '/change-email',
      '/link-social',
      '/unlink-account',
      '/get-access-token',
      '/refresh-token',
      '/account-info',
      '/email-otp/check-verification-otp',
      '/email-otp/verify-email',
      '/email-otp/request-password-reset',
      '/email-otp/reset-password',
      '/email-otp/request-email-change',
      '/email-otp/change-email',
      '/forget-password/email-otp',
    ],
    session: {
      expiresIn: 7 * 86400,
      updateAge: 86400,
      freshAge: 600,
      cookieCache: { enabled: false },
      additionalFields: { signInMethod },
    },
    account: {
      accountLinking: { enabled: true, trustedProviders: ['google'], allowDifferentEmails: false },
      encryptOAuthTokens: true,
    },
    verification: { storeIdentifier: 'hashed' },
    rateLimit: {
      enabled: true,
      storage: 'database',
      window: 60,
      max: 300,
      customRules: {
        '/email-otp/send-verification-otp': { window: 60, max: 3 },
        '/sign-in/email-otp': { window: 60, max: 5 },
        '/sign-in/social': { window: 60, max: 10 },
        '/device/code': { window: 60, max: 5 },
        '/device/token': { window: 60, max: 20 },
        '/device/approve': { window: 60, max: 10 },
        '/device/deny': { window: 60, max: 10 },
        '/organization/get-invitation': { window: 60, max: 10 },
        '/organization/accept-invitation': { window: 60, max: 10 },
        '/organization/invite-member': { window: 60, max: 20 },
      },
    },
    advanced: {
      cookiePrefix: 'goal',
      useSecureCookies: settings.publicURL.startsWith('https:'),
      defaultCookieAttributes: { httpOnly: true, sameSite: 'lax', path: '/' },
      // goald's proxy drops every forwarding header it receives and writes this one itself.
      ipAddress: { ipAddressHeaders: ['x-goal-client-ip'], ipv6Subnet: 64 },
      trustedProxyHeaders: false,
      // The origin and CSRF checks (Origin and callback URLs against trustedOrigins) stay on
      // whatever the environment says: Better Auth turns both off by default under NODE_ENV=test
      // or TEST, and goal up passes NODE_ENV through.
      disableOriginCheck: false,
      disableCSRFCheck: false,
      database: { generateId: 'uuid' },
      backgroundTasks: {
        handler: (p) => {
          p.catch((e: unknown) => log.log('error', 'background_task.failed', { error: e }));
        },
      },
    },
    logger: {
      level: 'warn',
      log: (level, message) => log.log(level, 'better_auth', { message }),
    },
    socialProviders: settings.google
      ? {
          google: {
            clientId: settings.google.clientId,
            clientSecret: settings.google.clientSecret,
            prompt: 'select_account',
            // No accessType: Google issues no refresh token (no offline access).
            getUserInfo: googleUserInfo,
          },
        }
      : {},
    databaseHooks: {
      user: { create: { before: signIn.allowOnlyInvitedOrBootstrap, after: signIn.recordUser } },
      session: {
        create: { before: signIn.tagSignInMethod, after: signIn.recordSignInAndClaimBootstrap },
        delete: {
          after: async (session, ctx) => {
            if (!ctx?.path || !revocations.has(ctx.path)) return;
            await db
              .record({ kind: 'session.revoked', user: session.userId, data: { via: ctx.path } })
              .catch((e: unknown) => log.log('error', 'auth_event.failed', { path: ctx.path, error: e }));
          },
        },
      },
      // Goal never acts on a person's Google account, so it keeps no refresh token.
      account: {
        create: { before: async (account) => ({ data: { ...account, ...noRefreshToken } }) },
        update: { before: async (account) => ({ data: { ...account, ...noRefreshToken } }) },
      },
    },
    hooks: { before: hooks.before, after: hooks.after },
    onAPIError: { errorURL: `${settings.publicURL}/sign-in` },
    telemetry: { enabled: false },
    plugins: [
      ...(mailer
        ? [
            emailOTP({
              otpLength: 6,
              expiresIn: 300,
              allowedAttempts: 3,
              storeOTP: 'hashed',
              // false on purpose: an invitee has no user until their first sign-in. The user-create
              // hook is the single gate (spec 3 §8.7).
              disableSignUp: false,
              sendVerificationOTP: signIn.sendCodeIfAllowed,
            }),
          ]
        : []),
      organization({
        ac,
        roles,
        allowUserToCreateOrganization: false,
        disableOrganizationDeletion: true,
        creatorRole: 'owner',
        invitationExpiresIn: 72 * 3600,
        requireEmailVerificationOnInvitation: true,
        cancelPendingInvitationsOnReInvite: true,
        invitationLimit: 100,
        membershipLimit: 1000,
        teams: { enabled: false },
        ...hooks.organization,
        schema: {
          member: { additionalFields: { changedBy, changedAt } },
          invitation: { additionalFields: { factoryRoles, changedBy, changedAt } },
        },
      }),
      deviceAuthorization({
        expiresIn: '10m',
        interval: '5s',
        userCodeLength: 8,
        validateClient: (id) => id === cliClientId,
        verificationUri: '/device',
      }),
      bearer(),
    ],
  });
}

export type Auth = ReturnType<typeof createAuth>;

const noRefreshToken = { refreshToken: null, refreshTokenExpiresAt: null };

type GoogleUserInfo = NonNullable<GoogleOptions['getUserInfo']>;

/**
 * googleUserInfo reads the person from the ID token Google's token endpoint returned (over TLS, to
 * this service). Only a Google-verified email is accepted, since an account and email codes with
 * the same email are one user (accountLinking.trustedProviders); no picture is kept (the UI shows
 * initials).
 */
const googleUserInfo: GoogleUserInfo = async (tokens) => {
  const claims = tokens.idToken ? decodeClaims(tokens.idToken) : undefined;
  if (typeof claims?.sub !== 'string' || typeof claims.email !== 'string' || claims.email_verified !== true) {
    return null;
  }
  return {
    user: { email: claims.email, name: typeof claims.name === 'string' ? claims.name : '', emailVerified: true },
    // The account is keyed by the profile's sub (the provider's accountSubject).
    data: claims as unknown as GoogleProfile,
  };
};

function decodeClaims(jwt: string): Record<string, unknown> | undefined {
  try {
    const claims: unknown = JSON.parse(Buffer.from(jwt.split('.')[1] ?? '', 'base64url').toString('utf8'));
    return typeof claims === 'object' && claims !== null ? (claims as Record<string, unknown>) : undefined;
  } catch {
    return undefined;
  }
}
