// Goal's rules on Better Auth's member, invitation, account and device endpoints (spec 3 §8.6,
// threat model §11.1 rows 9, 14, 15, 29 and 30): fresh sessions for sensitive changes, targets
// named explicitly, the author stamp the audit triggers read, owners protected, invitations bound
// to their verified recipient with links only to the public URL, factory roles granted on accept,
// the name-only profile edit, and the security log of device approvals and sign-outs.
import { randomBytes, randomUUID } from 'node:crypto';
import pg from 'pg';
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { cliClientId } from './auth.js';
import { userCodeSubject } from './goal.js';
import { freshFor } from './hooks.js';
import type { Logger } from './log.js';
import { MemoryMailer } from './mail/memory.js';
import { createTestDatabase, type TestDatabase } from './testing/database.js';
import { Fixtures, type Harness, harness } from './testing/service.js';

let db: TestDatabase;
let pool: pg.Pool;
let admin: pg.Pool;
let fx: Fixtures;

beforeAll(async () => {
  db = await createTestDatabase();
  pool = new pg.Pool({ connectionString: db.url('goal_auth') });
  admin = new pg.Pool({ connectionString: db.url() });
  fx = new Fixtures(admin);
});

afterAll(async () => {
  await Promise.all([pool?.end(), admin?.end()]);
  await db?.drop();
});

interface Person {
  id: string;
  email: string;
  member: string;
  cookie: string;
}

/** An organization with an owner, an admin and a member, each signed in with a fresh session. */
interface Org {
  id: string;
  factory: string;
  owner: Person;
  admin: Person;
  member: Person;
  h: Harness;
  mailer: MemoryMailer;
  lines: string[];
}

let o: Org;

beforeEach(async () => {
  const slug = `org-${randomBytes(4).toString('hex')}`;
  const id = await fx.org(slug, 'Acme');
  const factory = await fx.factory(id, 'Main line');
  const mailer = new MemoryMailer();
  const lines: string[] = [];
  const log: Logger = { log: (level, event, fields) => lines.push(JSON.stringify({ level, event, ...fields })) };
  const h = harness(pool, { mailer, log });
  const person = async (name: string, role: string): Promise<Person> => {
    const email = `${name.toLowerCase()}-${slug}@example.com`;
    const user = await fx.user(email, name);
    const member = await fx.member(id, user, role);
    return { id: user, email, member, cookie: (await h.signIn(email)).cookie };
  };
  o = {
    id,
    factory,
    owner: await person('Olu', 'owner'),
    admin: await person('Ada', 'admin'),
    member: await person('Mo', 'member'),
    h,
    mailer,
    lines,
  };
});

const as = (p: Person, extra: Record<string, string> = {}) => ({ cookie: p.cookie, ...extra });

/** audit is the organization's audit entries of action: who, about whom, with what. */
async function audit(action: string) {
  const { rows } = await admin.query(
    `SELECT actor_kind || ':' || actor_id AS by, target_id AS target, data FROM audit_log
     WHERE org_id = $1 AND action = $2 ORDER BY id`,
    [o.id, action],
  );
  return rows;
}

async function member(id: string) {
  const { rows } = await admin.query(
    `SELECT role, "changedBy" AS by, "changedAt" AS at FROM auth.member WHERE id = $1`,
    [id],
  );
  return rows[0];
}

describe('a sensitive change', () => {
  it.each([
    ['/organization/invite-member', () => ({ organizationId: o.id, email: 'x@example.com', role: 'member' })],
    ['/organization/cancel-invitation', () => ({ invitationId: randomUUID() })],
    ['/organization/remove-member', () => ({ organizationId: o.id, memberIdOrEmail: o.member.member })],
    ['/organization/update-member-role', () => ({ organizationId: o.id, memberId: o.member.member, role: 'admin' })],
    ['/device/approve', () => ({ userCode: 'ABCD1234' })],
  ])('%s needs a session signed in within 10 minutes', async (path, body) => {
    await fx.age(o.owner.id, freshFor + 60_000);

    const res = await o.h.post(path, body(), as(o.owner));

    expect(res.status).toBe(403);
    expect(await res.json()).toMatchObject({ code: 'REAUTH_REQUIRED' });
    expect((await member(o.member.member)).role).toBe('member');
  });

  it.each([
    ['/organization/invite-member', { email: 'x@example.com', role: 'member' }],
    ['/organization/update-member-role', { memberId: randomUUID(), role: 'admin' }],
    ['/organization/leave', {}],
  ])('%s must name its organization: the active one is never used', async (path, body) => {
    const res = await o.h.post(path, body, as(o.owner));

    expect(res.status).toBe(400);
    expect(await res.json()).toMatchObject({ code: 'ID_REQUIRED' });
  });
});

describe('the owner rules', () => {
  it.each([
    ['demote an owner', () => ({ memberId: o.owner.member, role: 'admin' })],
    ['make an owner', () => ({ memberId: o.member.member, role: 'owner' })],
  ])('an admin cannot %s', async (_, body) => {
    const res = await o.h.post('/organization/update-member-role', { organizationId: o.id, ...body() }, as(o.admin));

    expect(res.status).toBe(403);
    expect((await member(o.owner.member)).role).toBe('owner');
    expect((await member(o.member.member)).role).toBe('member');
  });

  it('an admin cannot remove an owner or invite one', async () => {
    const remove = await o.h.post(
      '/organization/remove-member',
      { organizationId: o.id, memberIdOrEmail: o.owner.email },
      as(o.admin),
    );
    const invite = await o.h.post(
      '/organization/invite-member',
      { organizationId: o.id, email: 'boss@example.com', role: 'owner' },
      as(o.admin),
    );

    expect(remove.status).toBe(403);
    expect(invite.status).toBe(403);
    expect((await member(o.owner.member)).role).toBe('owner');
  });

  it('a member has exactly one role', async () => {
    const res = await o.h.post(
      '/organization/update-member-role',
      { organizationId: o.id, memberId: o.member.member, role: 'admin,member' },
      as(o.owner),
    );

    expect(res.status).toBe(400);
    expect(await res.json()).toMatchObject({ code: 'ONE_ROLE' });
    await expect(
      admin.query(`UPDATE auth.member SET role = 'superuser' WHERE id = $1`, [o.member.member]),
    ).rejects.toThrow(/member_role_check/);
  });

  it('an owner’s change is stamped with the owner from the session, whatever the body says', async () => {
    const res = await o.h.post(
      '/organization/update-member-role',
      { organizationId: o.id, memberId: o.admin.member, role: 'member', changedBy: 'user:mallory' },
      as(o.owner),
    );

    expect(res.status).toBe(200);
    const m = await member(o.admin.member);
    expect(m).toMatchObject({ role: 'member', by: `user:${o.owner.id}` });
    expect(Date.now() - new Date(m.at).getTime()).toBeLessThan(60_000);
    expect(await audit('member.role_changed')).toEqual([
      { by: `user:${o.owner.id}`, target: o.admin.id, data: { user_id: o.admin.id, from: 'admin', to: 'member' } },
    ]);
  });

  it('a removal is audited as the admin who made it', async () => {
    const res = await o.h.post(
      '/organization/remove-member',
      { organizationId: o.id, memberIdOrEmail: o.member.email },
      as(o.admin),
    );

    expect(res.status).toBe(200);
    expect(await member(o.member.member)).toBeUndefined();
    expect(await audit('member.removed')).toEqual([
      { by: `user:${o.admin.id}`, target: o.member.id, data: { user_id: o.member.id, role: 'member', left: false } },
    ]);
  });

  it('leaving is audited as the member who left', async () => {
    const res = await o.h.post('/organization/leave', { organizationId: o.id }, as(o.member));

    expect(res.status).toBe(200);
    expect(await audit('member.removed')).toEqual([
      { by: `user:${o.member.id}`, target: o.member.id, data: { user_id: o.member.id, role: 'member', left: true } },
    ]);
  });
});

describe('an invitation', () => {
  async function invite(body: Record<string, unknown>, headers: Record<string, string> = {}) {
    return o.h.post(
      '/organization/invite-member',
      { organizationId: o.id, role: 'member', ...body },
      as(o.owner, headers),
    );
  }

  it('links to GOAL_PUBLIC_URL whatever Host the request names, stamped with the inviter', async () => {
    const email = `new-${o.id}@example.com`;
    const res = await invite(
      { email, factoryRoles: JSON.stringify([{ factoryId: o.factory.toUpperCase(), role: 'approver' }]) },
      { host: 'evil.example.com', 'x-forwarded-host': 'evil.example.com', 'x-forwarded-proto': 'https' },
    );

    expect(res.status).toBe(200);
    const { id } = (await res.json()) as { id: string };
    await vi.waitFor(() => expect(o.mailer.to(email)).toHaveLength(1));
    const [mail] = o.mailer.to(email);
    expect(mail?.text).toContain(`Join: http://127.0.0.1:8080/invite/${id} · expires `);
    expect(mail?.html).not.toContain('evil.example.com');
    expect(mail?.subject).toBe('Olu invited you to Acme on Goal');

    const { rows } = await admin.query(
      `SELECT "factoryRoles" AS roles, "changedBy" AS by FROM auth.invitation WHERE id = $1`,
      [id],
    );
    expect(rows[0]).toEqual({
      roles: JSON.stringify([{ factoryId: o.factory, role: 'approver' }]),
      by: `user:${o.owner.id}`,
    });
  });

  it.each([
    ['a factory of no organization of theirs', () => [{ factoryId: randomUUID(), role: 'viewer' }], 'member'],
    ['a role that is not a factory role', () => [{ factoryId: o.factory, role: 'owner' }], 'member'],
    ['factory roles for an admin', () => [{ factoryId: o.factory, role: 'viewer' }], 'admin'],
  ])('refuses %s', async (_, roles, role) => {
    const res = await invite({ email: `no-${o.id}@example.com`, role, factoryRoles: JSON.stringify(roles()) });

    expect(res.status).toBe(400);
  });

  it('shows its recipient who sent it and the factories it gives, by name, and nobody else', async () => {
    const email = `shown-${o.id}@example.com`;
    const created = await invite({ email, factoryRoles: JSON.stringify([{ factoryId: o.factory, role: 'viewer' }]) });
    const { id: invitation } = (await created.json()) as { id: string };
    const invitee = await harness(pool, { mailer: o.mailer }).signIn(email);

    const res = await o.h.get(`/organization/get-invitation?id=${invitation}`, { cookie: invitee.cookie });
    expect(res.status).toBe(200);
    expect(await res.json()).toMatchObject({
      id: invitation,
      organizationId: o.id,
      organizationName: 'Acme',
      role: 'member',
      inviterName: 'Olu',
      factories: [{ id: o.factory, name: 'Main line', role: 'viewer' }],
    });

    const other = await o.h.get(`/organization/get-invitation?id=${invitation}`, as(o.member));
    expect(other.status).toBe(403);
  });

  it('is accepted by its recipient only, who then holds its factory roles', async () => {
    const email = `invitee-${o.id}@example.com`;
    const created = await invite({ email, factoryRoles: JSON.stringify([{ factoryId: o.factory, role: 'editor' }]) });
    const { id: invitation } = (await created.json()) as { id: string };

    const stranger = await o.h.post('/organization/accept-invitation', { invitationId: invitation }, as(o.member));
    expect(stranger.status).toBe(403);

    // From a client of their own: the organization's three sign-ins used this one's code requests.
    const invitee = await harness(pool, { mailer: o.mailer }).signIn(email);
    expect(invitee.res.status).toBe(200);
    const accept = await o.h.post(
      '/organization/accept-invitation',
      { invitationId: invitation },
      { cookie: invitee.cookie },
    );
    expect(accept.status).toBe(200);

    const { rows } = await admin.query(
      `SELECT f.role, f.granted_by, i."changedBy" AS by FROM factory_roles f
       JOIN auth."user" u ON u.id = f.user_id JOIN auth.invitation i ON i.id = $2
       WHERE f.org_id = $1 AND u.email = $3`,
      [o.id, invitation, email],
    );
    const { rows: invitees } = await admin.query(`SELECT id FROM auth."user" WHERE email = $1`, [email]);
    const inviteeId = invitees[0]?.id;
    expect(rows).toEqual([{ role: 'editor', granted_by: `user:${o.owner.id}`, by: `user:${inviteeId}` }]);
    expect(await audit('invitation.accepted')).toEqual([
      { by: `user:${inviteeId}`, target: invitation, data: { invitation_id: invitation, email } },
    ]);
    expect((await audit('member.added')).filter((e) => e.target === inviteeId)).toEqual([
      expect.objectContaining({
        by: `user:${inviteeId}`,
        data: { user_id: inviteeId, role: 'member', via: 'invitation' },
      }),
    ]);
    expect(await audit('factory_role.granted')).toEqual([
      {
        by: `user:${o.owner.id}`,
        target: inviteeId,
        data: { user_id: inviteeId, factory_id: o.factory, to: 'editor', via: 'invitation' },
      },
    ]);

    // The invitation id and the addresses never reach the logs (spec 3 §11.1 row 14, row 24).
    const logs = o.lines.join('\n');
    expect(logs).not.toContain(invitation);
    expect(logs).not.toContain('@example.com');
  });

  it('is audited as made by its inviter and cancelled by whoever cancels it', async () => {
    const email = `cancel-${o.id}@example.com`;
    const created = await invite({ email });
    const { id: invitation } = (await created.json()) as { id: string };

    const cancel = await o.h.post('/organization/cancel-invitation', { invitationId: invitation }, as(o.admin));

    expect(cancel.status).toBe(200);
    expect(await audit('invitation.created')).toEqual([
      expect.objectContaining({ by: `user:${o.owner.id}`, target: invitation }),
    ]);
    expect(await audit('invitation.canceled')).toEqual([
      { by: `user:${o.admin.id}`, target: invitation, data: { invitation_id: invitation, email } },
    ]);
  });

  it('is not accepted by an account whose email is not verified', async () => {
    const email = `unverified-${o.id}@example.com`;
    const created = await invite({ email });
    const { id: invitation } = (await created.json()) as { id: string };
    const user = await fx.user(email, 'Una', false);
    const token = randomBytes(24).toString('base64url').replace(/[-_]/g, 'a').slice(0, 32);
    await admin.query(
      `INSERT INTO auth.session (token, "userId", "expiresAt", "updatedAt") VALUES ($1, $2, now() + interval '1 day', now())`,
      [token, user],
    );

    const res = await o.h.post(
      '/organization/accept-invitation',
      { invitationId: invitation },
      { authorization: `Bearer ${token}` },
    );

    expect(res.status).toBe(403);
    expect(await res.json()).toMatchObject({
      code: 'EMAIL_VERIFICATION_REQUIRED_BEFORE_ACCEPTING_OR_REJECTING_INVITATION',
    });
  });
});

describe('editing one’s profile', () => {
  it('changes the name, trimmed', async () => {
    const res = await o.h.post('/update-user', { name: '  Olu A.  ' }, as(o.owner));

    expect(res.status).toBe(200);
    const { rows } = await admin.query(`SELECT name FROM auth."user" WHERE id = $1`, [o.owner.id]);
    expect(rows[0].name).toBe('Olu A.');
  });

  it.each([
    ['another field', { name: 'Olu', image: 'https://example.com/me.png' }],
    ['an empty name', { name: '   ' }],
    ['a name over 80 characters', { name: 'x'.repeat(81) }],
  ])('refuses %s', async (_, body) => {
    const res = await o.h.post('/update-user', body, as(o.owner));

    expect(res.status).toBe(400);
    expect(await res.json()).toMatchObject({ code: 'NAME_ONLY' });
  });
});

describe('the security log', () => {
  it('records a device sign-in request, its approval by the session’s user, and sign-out', async () => {
    const code = await o.h.post('/device/code', { client_id: cliClientId });
    const { user_code: userCode } = (await code.json()) as { user_code: string };
    await o.h.get(`/device?user_code=${userCode}`, as(o.owner));
    const approve = await o.h.post('/device/approve', { userCode }, as(o.owner));
    expect(approve.status).toBe(200);
    const out = await o.h.post('/sign-out', {}, as(o.owner));
    expect(out.status).toBe(200);

    const events = async (kind: string) =>
      (await fx.events(kind)).filter((e) => e.ip === o.h.ip || e.user_id === o.owner.id);
    expect(await events('device.requested')).toEqual([
      expect.objectContaining({ subject: userCodeSubject(userCode), data: { client_id: cliClientId } }),
    ]);
    expect(await events('device.approved')).toEqual([
      expect.objectContaining({ user_id: o.owner.id, subject: userCodeSubject(userCode) }),
    ]);
    expect(await events('session.revoked')).toEqual([
      expect.objectContaining({ user_id: o.owner.id, data: { via: '/sign-out' } }),
    ]);
  });
});
