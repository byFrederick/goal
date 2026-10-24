import { betterAuth } from 'better-auth';
import { bearer } from 'better-auth/plugins/bearer';
import { deviceAuthorization } from 'better-auth/plugins/device-authorization';
import { emailOTP } from 'better-auth/plugins/email-otp';
import { organization } from 'better-auth/plugins/organization';
import { PostgresDialect } from 'kysely';
import type pg from 'pg';
import { ac, roles } from './access.gen.js';
import type { Logger } from './log.js';
import type { Settings } from './settings.js';

/** The schema of Better Auth's tables (migrations 00040, 00041). */
export const authSchema = 'auth';

/** The one client the device authorization grant serves: `goal login`. */
export const cliClientId = 'goal-cli';

/** Sends a sign-in code to an address. It never logs the code. */
export type SendSignInCode = (email: string, code: string) => Promise<void>;

export interface AuthDeps {
  settings: Settings;
  /** The database as goal_auth (checkRole has passed). */
  pool: pg.Pool;
  log: Logger;
  sendSignInCode: SendSignInCode;
}

// Fields Goal adds to Better Auth's member and invitation rows (spec 3 §6.2). changedBy holds the
// actor ref of the last change and changedAt its time, so the audit triggers name the person who
// made it; neither is accepted from a request. factoryRoles is an invitation's JSON
// [{"factoryId","role"}], applied when it is accepted.
const changedBy = { type: 'string', required: false, input: false } as const;
const changedAt = { type: 'date', required: false, input: false } as const;
const factoryRoles = { type: 'string', required: false, input: true } as const;

/**
 * createAuth configures Better Auth for Goal (spec 3 §8.6): passwordless sign-in by email code or
 * Google, organizations whose lifecycle goald owns (Better Auth's create, update and delete paths
 * are off), the device grant for `goal login`, and bearer tokens for the CLI. Its tables live in
 * schema auth under goose: `auth generate` (task auth:schema-check) must find nothing to change.
 * Sign-up is closed: no user is created until the invitation and bootstrap gate replaces the
 * refusal below (spec 3 §8.7).
 */
export function createAuth({ settings, pool, log, sendSignInCode }: AuthDeps) {
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
    session: { expiresIn: 7 * 86400, updateAge: 86400, freshAge: 600, cookieCache: { enabled: false } },
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
          },
        }
      : {},
    databaseHooks: {
      user: { create: { before: async () => false } },
    },
    onAPIError: { errorURL: `${settings.publicURL}/sign-in` },
    telemetry: { enabled: false },
    plugins: [
      emailOTP({
        otpLength: 6,
        expiresIn: 300,
        allowedAttempts: 3,
        storeOTP: 'hashed',
        disableSignUp: false,
        sendVerificationOTP: async ({ email, otp, type }) => {
          if (type === 'sign-in') await sendSignInCode(email, otp);
        },
      }),
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
