// Goal's hooks on Better Auth's endpoints (spec 3 §8.6): the before-hook refuses what Goal does not
// allow and stamps the author of a membership change, the after-hook writes the security log, and
// the organization hooks check invitations and grant their factory roles. Every id a hook acts as
// comes from the session, never from the request body (§11.1 row 29).
import { APIError, type AuthMiddleware, createAuthMiddleware, getSessionFromCtx, isAPIError } from 'better-auth/api';
import type { OrganizationOptions } from 'better-auth/plugins/organization';
import { type Clock, type GoalDatabase, userCodeSubject } from './goal.js';
import type { Logger } from './log.js';
import type { Mailer } from './mail/mailer.js';
import { invitationMail } from './mail/messages.js';
import { browserSignIn, client, deliver, type SignIn } from './signin.js';

/** Sensitive changes need a session signed in within this long (spec 3 §8.4, freshAge). */
export const freshFor = 10 * 60_000;

const orgRoles = ['owner', 'admin', 'member'];
const factoryRoles = ['viewer', 'approver', 'editor'];
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

type HookContext = Parameters<Parameters<typeof createAuthMiddleware>[0]>[0];

/**
 * The endpoints whose rules read the caller's session. Better Auth runs this before-hook ahead of
 * the bearer plugin's, which turns a bearer token into the session cookie, so the hook would see no
 * session there and hold it to none of the rules (fresh, owners, the stamp): a bearer credential is
 * refused on them (review M1). The browser sends its cookie; the CLI never calls them.
 */
const cookieOnly = new Set([
  '/update-user',
  '/device/approve',
  '/organization/invite-member',
  '/organization/cancel-invitation',
  '/organization/accept-invitation',
  '/organization/reject-invitation',
  '/organization/remove-member',
  '/organization/update-member-role',
  '/organization/leave',
]);
type Body = Record<string, unknown>;

export interface HookDeps {
  db: GoalDatabase;
  signIn: SignIn;
  mailer: Mailer;
  log: Logger;
  clock: Clock;
  publicURL: string;
}

function refuse(code: string, message: string, status: 'BAD_REQUEST' | 'FORBIDDEN' = 'BAD_REQUEST'): APIError {
  return new APIError(status, { code, message });
}

function body(ctx: HookContext): Body {
  return typeof ctx.body === 'object' && ctx.body !== null ? (ctx.body as Body) : {};
}

/** id is body[field] when it is a UUID; organization endpoints must name their target (§8.6). */
function id(b: Body, field: string): string {
  const v = b[field];
  if (typeof v !== 'string' || !uuid.test(v)) throw refuse('ID_REQUIRED', `${field} must name the target`);
  return v.toLowerCase();
}

/** oneRole refuses anything but exactly one organization role. */
function oneRole(role: unknown): string {
  if (typeof role !== 'string' || !orgRoles.includes(role)) {
    throw refuse('ONE_ROLE', 'A member has exactly one role: owner, admin or member');
  }
  return role;
}

export function goalHooks({ db, signIn, mailer, log, clock, publicURL }: HookDeps) {
  /** session is the caller's session, or null (Better Auth then refuses the request itself). */
  const session = (ctx: HookContext) => getSessionFromCtx(ctx);

  /** fresh is the caller's session when it was signed in within freshFor, else a refusal. */
  const fresh = async (ctx: HookContext) => {
    const s = await session(ctx);
    if (s && clock().getTime() - new Date(s.session.createdAt).getTime() > freshFor) {
      throw refuse('REAUTH_REQUIRED', 'Sign in again to do this', 'FORBIDDEN');
    }
    return s;
  };

  /** ownerRule: only an owner changes an owner or makes one (Better Auth's rule, kept here too). */
  const ownerRule = async (org: string, actor: string, member: string, newRole?: string) => {
    const roles = await db.roles(org, actor, member);
    if ((roles.member === 'owner' || newRole === 'owner') && roles.actor !== 'owner') {
      throw refuse('OWNERS_ONLY', 'Only an owner can change an owner', 'FORBIDDEN');
    }
  };

  const before: AuthMiddleware = createAuthMiddleware(async (ctx) => {
    const b = body(ctx);
    if (cookieOnly.has(ctx.path) && (ctx.request?.headers.has('authorization') || ctx.headers?.has('authorization'))) {
      throw refuse('BROWSER_SESSION_REQUIRED', 'Do this in the browser, signed in there', 'FORBIDDEN');
    }
    switch (ctx.path) {
      case '/email-otp/send-verification-otp':
        if (b.type !== 'sign-in') throw refuse('SIGN_IN_ONLY', 'Goal sends codes for signing in only');
        return;
      case '/sign-in/email-otp':
        if (typeof b.email === 'string') await signIn.refuseWhileLocked(b.email.toLowerCase());
        return;
      case '/update-user': {
        const name = typeof b.name === 'string' ? b.name.trim() : '';
        if (Object.keys(b).some((k) => k !== 'name') || name.length < 1 || name.length > 80) {
          throw refuse('NAME_ONLY', 'Only the name can change, 1 to 80 characters');
        }
        return { context: { body: { name } } };
      }
      case '/device/approve': {
        // Only a person signed in through the browser approves a CLI sign-in: a goal login session never
        // does, fresh as it is, or it could keep itself fresh by approving codes of its own (rows 20,
        // 30). Checked before freshness, so it is not told to sign in again.
        const s = await session(ctx);
        if (s && !browserSignIn(s.session)) {
          throw refuse(
            'BROWSER_SESSION_REQUIRED',
            'Approve a CLI sign-in through the browser, signed in there',
            'FORBIDDEN',
          );
        }
        await fresh(ctx);
        return;
      }
      case '/organization/invite-member':
        id(b, 'organizationId');
        oneRole(b.role);
        await fresh(ctx);
        return;
      case '/organization/cancel-invitation': {
        const invitation = id(b, 'invitationId');
        const s = await fresh(ctx);
        if (s) await db.stampInvitation(invitation, s.user.id);
        return;
      }
      case '/organization/accept-invitation':
      case '/organization/reject-invitation': {
        const invitation = id(b, 'invitationId');
        const s = await session(ctx);
        if (s) await db.stampInvitation(invitation, s.user.id);
        return;
      }
      case '/organization/remove-member': {
        const org = id(b, 'organizationId');
        const member = b.memberIdOrEmail;
        if (typeof member !== 'string' || member === '')
          throw refuse('ID_REQUIRED', 'memberIdOrEmail must name the target');
        const s = await fresh(ctx);
        if (!s) return;
        await ownerRule(org, s.user.id, member);
        await db.stampMember(org, member, s.user.id);
        return;
      }
      case '/organization/update-member-role': {
        const org = id(b, 'organizationId');
        const member = id(b, 'memberId');
        const role = oneRole(b.role);
        const s = await fresh(ctx);
        if (!s) return;
        await ownerRule(org, s.user.id, member, role);
        await db.stampMember(org, member, s.user.id);
        return;
      }
      case '/organization/leave': {
        const org = id(b, 'organizationId');
        const s = await session(ctx);
        if (s) await db.stampOwnMember(org, s.user.id);
        return;
      }
    }
  });

  const after: AuthMiddleware = createAuthMiddleware(async (ctx) => {
    const returned = ctx.context.returned;
    const failed = isAPIError(returned) || (returned instanceof Response && !returned.ok);
    const b = body(ctx);
    if (sessionAnswers.has(ctx.path) && !failed) {
      const stripped = withoutSessionToken(returned);
      if (stripped) return ctx.json(stripped);
    }
    try {
      switch (ctx.path) {
        case '/sign-in/email-otp':
          if (failed && typeof b.email === 'string') await signIn.recordFailure(b.email.toLowerCase(), ctx);
          return;
        case '/device/code': {
          if (failed) return;
          const r = (returned instanceof Response ? await returned.clone().json() : returned) as Body;
          if (typeof r.user_code !== 'string') return;
          await db.record({
            kind: 'device.requested',
            subject: userCodeSubject(r.user_code),
            ...client(ctx),
            data: { client_id: b.client_id },
          });
          return;
        }
        case '/organization/get-invitation': {
          // Better Auth answered only its recipient (the session's email matches) about a pending
          // invitation: Invite-Accept also shows them who sent it and the factories it gives.
          if (failed) return;
          const r = (returned instanceof Response ? await returned.clone().json() : returned) as Body;
          return ctx.json({ ...r, ...(await invitationDetails(db, r)) });
        }
        case '/device/approve':
        case '/device/deny': {
          if (failed || typeof b.userCode !== 'string') return;
          await db.record({
            kind: ctx.path === '/device/approve' ? 'device.approved' : 'device.denied',
            user: ctx.context.session?.user.id,
            subject: userCodeSubject(b.userCode),
            ...client(ctx),
          });
          return;
        }
      }
    } catch (e) {
      log.log('error', 'auth_event.failed', { path: ctx.path, error: e });
    }
  });

  /** The organization plugin's hooks and invitation mail (spec 3 §8.6). */
  const organization = {
    sendInvitationEmail: async (data) => {
      await deliver(
        mailer,
        log,
        invitationMail(data.email, {
          publicURL,
          invitationId: data.id,
          inviterName: data.inviter.user.name,
          orgName: data.organization.name,
          expiresAt: new Date(data.invitation.expiresAt),
        }),
        { mail: 'invitation' },
      );
    },
    organizationHooks: {
      // One role, factory roles of the organization's factories only (members only), and the
      // inviter as the invitation's author for the audit trigger.
      beforeCreateInvitation: async ({ invitation, inviter, organization }) => {
        const role = oneRole(invitation.role);
        const grants = parseFactoryRoles(invitation.factoryRoles);
        if (grants.length > 0 && role !== 'member') {
          throw refuse('FACTORY_ROLES_FOR_MEMBERS', 'Owners and admins hold every factory role already');
        }
        const known = new Set((await db.orgFactories(organization.id)).map((f) => f.id));
        if (grants.some((g) => !known.has(g.factoryId))) throw refuse('UNKNOWN_FACTORY', 'No such factory here');
        return {
          data: {
            factoryRoles: grants.length > 0 ? JSON.stringify(grants) : null,
            changedBy: `user:${inviter.id}`,
            changedAt: new Date(),
          },
        };
      },
      beforeUpdateMemberRole: async ({ newRole }) => {
        oneRole(newRole);
      },
      // The member exists by now; a failure to grant is logged and the member keeps no factory
      // role, which an owner can fix in Members.
      afterAcceptInvitation: async ({ invitation }) => {
        try {
          await db.applyInvitationRoles(invitation.id);
        } catch (e) {
          log.log('error', 'invitation_roles.failed', { error: e });
        }
      },
    },
  } satisfies Pick<OrganizationOptions, 'sendInvitationEmail' | 'organizationHooks'>;

  return { before, after, organization };
}

/**
 * The endpoints that answer a session to the page that called them. Better Auth puts the session
 * token in the answer (get-session's session.token, a sign-in's token), where any script on the
 * page could read what the httpOnly cookie hides, and goald accepts the token as a bearer value or
 * an unsigned cookie: so the after-hook takes it out (spec 3 §11.1 row 5, review M2). goald's proxy
 * strips it again. goal login's /device/token keeps its access_token: that is the CLI's credential.
 */
const sessionAnswers = new Set(['/get-session', '/sign-in/email-otp', '/sign-in/social']);

/** withoutSessionToken is a session answer without its token(s); undefined when it holds none. */
export function withoutSessionToken(returned: unknown): Body | undefined {
  if (typeof returned !== 'object' || returned === null || returned instanceof Response) return undefined;
  const { token, ...rest } = returned as Body;
  const session = rest.session;
  if (typeof session === 'object' && session !== null && 'token' in session) {
    const { token: _, ...kept } = session as Body;
    return { ...rest, session: kept };
  }
  return token === undefined ? undefined : rest;
}

/**
 * invitationDetails names, for its recipient, who sent an invitation (inviterName) and the factories
 * it gives with their roles (factories: [{id, name, role}], of factories that still exist).
 */
async function invitationDetails(
  db: GoalDatabase,
  invitation: Body,
): Promise<{ inviterName: string | null; factories: { id: string; name: string; role: string }[] }> {
  const inviter = typeof invitation.inviterId === 'string' ? await db.userName(invitation.inviterId) : null;
  let grants: FactoryRoleGrant[] = [];
  try {
    grants = parseFactoryRoles(invitation.factoryRoles);
  } catch {
    // The hook checked them when the invitation was made; anything else shows no factory.
  }
  const org = typeof invitation.organizationId === 'string' ? invitation.organizationId : '';
  const names = new Map((grants.length > 0 ? await db.orgFactories(org) : []).map((f) => [f.id, f.name]));
  const factories = grants.flatMap((g) => {
    const name = names.get(g.factoryId);
    return name === undefined ? [] : [{ id: g.factoryId, name, role: g.role }];
  });
  return { inviterName: inviter, factories };
}

export interface FactoryRoleGrant {
  factoryId: string;
  role: string;
}

/**
 * parseFactoryRoles reads an invitation's factoryRoles, JSON [{"factoryId","role"}] or nothing,
 * one role per factory; anything else is refused.
 */
export function parseFactoryRoles(raw: unknown): FactoryRoleGrant[] {
  if (raw === undefined || raw === null || raw === '') return [];
  let parsed: unknown;
  try {
    parsed = typeof raw === 'string' ? JSON.parse(raw) : raw;
  } catch {
    throw refuse('FACTORY_ROLES', 'factoryRoles must be JSON');
  }
  if (!Array.isArray(parsed) || parsed.length > 1000) throw refuse('FACTORY_ROLES', 'factoryRoles must be a list');
  const grants = new Map<string, string>();
  for (const g of parsed) {
    const factoryId = typeof g?.factoryId === 'string' ? g.factoryId.toLowerCase() : '';
    if (!uuid.test(factoryId) || !factoryRoles.includes(g?.role) || grants.has(factoryId)) {
      throw refuse('FACTORY_ROLES', 'Each factory once, as viewer, approver or editor');
    }
    grants.set(factoryId, g.role);
  }
  return [...grants].map(([factoryId, role]) => ({ factoryId, role }));
}
