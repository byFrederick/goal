// Goal's side of the database for the auth service: the definer functions of migration 00044
// (the security log, its counts, the bootstrap claim, invitations' factory roles), of 00078 (sign-up
// by domain) and the few reads
// and stamps of Better Auth's own rows that Goal's rules need. Every query is static and
// parameterized; nothing here returns a code, a token or a hash.
import { createHash } from 'node:crypto';
import { isIP } from 'node:net';
import type pg from 'pg';

/** Clock is the time Goal's rules use, injected so tests can move it. */
export type Clock = () => Date;

/** The kinds of auth_events the auth service writes (spec 3 §6.4; goal_auth_event refuses others). */
export type AuthEventKind =
  | 'sign_in.succeeded'
  | 'sign_up.refused'
  | 'user.created'
  | 'otp.sent'
  | 'otp.suppressed'
  | 'otp.failed'
  | 'otp.locked'
  | 'device.requested'
  | 'device.approved'
  | 'device.denied'
  | 'session.revoked';

export interface AuthEvent {
  kind: AuthEventKind;
  user?: string | undefined;
  /** A subject from emailSubject or userCodeSubject, never the raw value. */
  subject?: string | undefined;
  /** The client address goald's proxy wrote; anything that is not an IP is dropped. */
  ip?: string | null | undefined;
  userAgent?: string | null | undefined;
  data?: Record<string, unknown>;
}

/** emailSubject is base64url(SHA-256(lower(email))), how the security log names an address. */
export function emailSubject(email: string): string {
  return sha256(email.trim().toLowerCase());
}

/** userCodeSubject names a device grant's user code (as typed, or as issued) in the security log. */
export function userCodeSubject(userCode: string): string {
  return sha256(userCode.replace(/[^a-zA-Z0-9]/g, '').toUpperCase());
}

function sha256(s: string): string {
  return createHash('sha256').update(s).digest('base64url');
}

export interface Factory {
  id: string;
  name: string;
}

export class GoalDatabase {
  constructor(private readonly pool: pg.Pool) {}

  async record(e: AuthEvent): Promise<void> {
    await this.pool.query('SELECT goal_auth_event($1, $2, $3, $4, $5, $6)', [
      e.kind,
      e.user ?? null,
      e.subject ?? null,
      e.ip && isIP(e.ip) ? e.ip : null,
      e.userAgent ?? null,
      e.data ?? {},
    ]);
  }

  /** count is how many events of kind the subject has had since since. */
  async count(kind: AuthEventKind, subject: string, since: Date): Promise<number> {
    const { rows } = await this.pool.query<{ n: number }>('SELECT goal_auth_count($1, $2, $3) AS n', [
      kind,
      subject,
      since,
    ]);
    return rows[0]?.n ?? 0;
  }

  /** claimBootstrap makes user the bootstrap owner while there is none; true when user is one. */
  async claimBootstrap(user: string): Promise<boolean> {
    const { rows } = await this.pool.query<{ ok: boolean }>('SELECT goal_claim_bootstrap($1) AS ok', [user]);
    return rows[0]?.ok === true;
  }

  /** orgFactories lists a live organization's factories; none for a deleted or unknown one. */
  async orgFactories(org: string): Promise<Factory[]> {
    const { rows } = await this.pool.query<{ f: Factory[] }>('SELECT goal_org_factories($1) AS f', [org]);
    return rows[0]?.f ?? [];
  }

  /** applyInvitationRoles grants an accepted invitation's factory roles; idempotent. */
  async applyInvitationRoles(invitation: string): Promise<void> {
    await this.pool.query('SELECT goal_apply_invitation_roles($1)', [invitation]);
  }

  /**
   * isInvitee is true when email has a pending invitation, unexpired at now, to a live organization
   * (goal_is_invitee, 00049): a deleted organization's invitations open no sign-up (review L1).
   */
  async isInvitee(email: string, now: Date): Promise<boolean> {
    const { rows } = await this.pool.query<{ ok: boolean }>('SELECT goal_is_invitee($1, $2) AS ok', [email, now]);
    return rows[0]?.ok === true;
  }

  /**
   * signupDomainOrg is the live organization whose sign-up domain is exactly email's domain, with
   * its join role (goal_signup_domain_org, 00078); null for any other address, a webmail domain or
   * a deleted organization.
   */
  async signupDomainOrg(email: string): Promise<{ org: string; joinRole: string } | null> {
    const { rows } = await this.pool.query<{ org_id: string; join_role: string }>(
      'SELECT org_id, join_role FROM goal_signup_domain_org($1)',
      [email],
    );
    const row = rows[0];
    return row ? { org: row.org_id, joinRole: row.join_role } : null;
  }

  /**
   * applySignupDomain joins user, by their verified email, to the organization of its sign-up
   * domain with its join role (goal_apply_signup_domain, 00078) and returns that organization; null
   * when there is none, they are invited to it, or they are already a member. Idempotent.
   */
  async applySignupDomain(user: string): Promise<string | null> {
    const { rows } = await this.pool.query<{ org: string | null }>('SELECT goal_apply_signup_domain($1) AS org', [
      user,
    ]);
    return rows[0]?.org ?? null;
  }

  /** orgIsLive is true when org is an organization that has not been deleted (goal_org_live). */
  async orgIsLive(org: string): Promise<boolean> {
    const { rows } = await this.pool.query<{ ok: boolean }>('SELECT goal_org_live($1) AS ok', [org]);
    return rows[0]?.ok === true;
  }

  /**
   * userName is the name of the user with this id, or their email while they have none (signed up
   * by code); null for no such user.
   */
  async userName(id: string): Promise<string | null> {
    const { rows } = await this.pool.query<{ name: string }>(
      `SELECT coalesce(nullif(btrim(name), ''), email) AS name FROM auth."user" WHERE id = $1`,
      [id],
    );
    return rows[0]?.name ?? null;
  }

  /** isUser is true when an account has this email. */
  async isUser(email: string): Promise<boolean> {
    const { rows } = await this.pool.query('SELECT 1 FROM auth."user" WHERE email = lower($1)', [email.trim()]);
    return rows.length > 0;
  }

  async hasUsers(): Promise<boolean> {
    const { rows } = await this.pool.query('SELECT EXISTS (SELECT 1 FROM auth."user") AS any');
    return rows[0]?.any === true;
  }

  /**
   * roles are actor's role in org and that of the member named by its id or its user's email
   * (null when either is not a member of org).
   */
  async roles(org: string, actor: string, member: string): Promise<{ actor: string | null; member: string | null }> {
    const { rows } = await this.pool.query<{ actor: string | null; member: string | null }>(
      `SELECT (SELECT role FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2::uuid) AS actor,
              (SELECT m.role FROM auth.member m WHERE m."organizationId" = $1
                 AND (m.id::text = lower($3) OR m."userId" = (SELECT u.id FROM auth."user" u WHERE u.email = lower($3)))) AS member`,
      [org, actor, member],
    );
    return rows[0] ?? { actor: null, member: null };
  }

  /**
   * stampMember records actor as the author of the coming change of a member of org, named by its
   * id or its user's email, so the audit trigger names them (spec 3 §6.4). It stamps only for an
   * actor who is a member of org; Better Auth then decides whether the change is allowed.
   */
  async stampMember(org: string, member: string, actor: string): Promise<void> {
    await this.pool.query(
      `UPDATE auth.member m SET "changedBy" = 'user:' || $3::uuid, "changedAt" = now()
       WHERE m."organizationId" = $1
         AND (m.id::text = lower($2) OR m."userId" = (SELECT u.id FROM auth."user" u WHERE u.email = lower($2)))
         AND EXISTS (SELECT 1 FROM auth.member a WHERE a."organizationId" = $1 AND a."userId" = $3::uuid)`,
      [org, member, actor],
    );
  }

  /** stampOwnMember records actor as the author of the coming change of their own membership. */
  async stampOwnMember(org: string, actor: string): Promise<void> {
    await this.pool.query(
      `UPDATE auth.member SET "changedBy" = 'user:' || $2::uuid, "changedAt" = now()
       WHERE "organizationId" = $1 AND "userId" = $2::uuid`,
      [org, actor],
    );
  }

  /**
   * stampInvitation records actor as the author of the coming change of an invitation: a member
   * of its organization (cancel) or its invitee (accept, reject).
   */
  async stampInvitation(invitation: string, actor: string): Promise<void> {
    await this.pool.query(
      `UPDATE auth.invitation i SET "changedBy" = 'user:' || $2::uuid, "changedAt" = now()
       WHERE i.id = $1
         AND (EXISTS (SELECT 1 FROM auth.member a WHERE a."organizationId" = i."organizationId" AND a."userId" = $2::uuid)
              OR EXISTS (SELECT 1 FROM auth."user" u WHERE u.id = $2::uuid AND u.email = lower(i.email)))`,
      [invitation, actor],
    );
  }
}
