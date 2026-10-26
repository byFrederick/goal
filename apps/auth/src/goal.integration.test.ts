// Migration 00044's definer functions, called as the auth service calls them (goal_auth).
import pg from 'pg';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { emailSubject, GoalDatabase } from './goal.js';
import { createTestDatabase, type TestDatabase } from './testing/database.js';
import { Fixtures } from './testing/service.js';

let db: TestDatabase;
let auth: pg.Pool;
let admin: pg.Pool;
let goal: GoalDatabase;
let fx: Fixtures;

beforeAll(async () => {
  db = await createTestDatabase();
  auth = new pg.Pool({ connectionString: db.url('goal_auth') });
  admin = new pg.Pool({ connectionString: db.url() });
  goal = new GoalDatabase(auth);
  fx = new Fixtures(admin);
});

afterAll(async () => {
  await Promise.all([auth?.end(), admin?.end()]);
  await db?.drop();
});

const functions = [
  'goal_auth_event(text, uuid, text, inet, text, jsonb)',
  'goal_auth_count(text, text, timestamptz)',
  'goal_claim_bootstrap(uuid)',
  'goal_org_factories(uuid)',
  'goal_apply_invitation_roles(uuid)',
];

describe('the auth service functions', () => {
  it.each(functions)('%s runs as its owner with a pinned search_path, for goal_auth only', async (fn) => {
    const { rows } = await admin.query<{
      definer: boolean;
      config: string[];
      auth: boolean;
      app: boolean;
      public: boolean;
    }>(
      `SELECT p.prosecdef AS definer, p.proconfig AS config,
              has_function_privilege('goal_auth', p.oid, 'EXECUTE') AS auth,
              has_function_privilege('goal_app', p.oid, 'EXECUTE') AS app,
              EXISTS (SELECT 1 FROM aclexplode(p.proacl) a WHERE a.grantee = 0) AS public
       FROM pg_proc p WHERE p.oid = $1::regprocedure`,
      [fn],
    );
    expect(rows[0]).toMatchObject({ definer: true, auth: true, app: false, public: false });
    expect(rows[0]?.config.find((c) => c.startsWith('search_path='))).toMatch(/, pg_temp$/);
  });
});

describe('goal_auth_event and goal_auth_count', () => {
  it('record the auth service’s events and count them by kind, subject and time', async () => {
    const subject = emailSubject('Count@Example.com');
    const before = new Date(Date.now() - 1000);
    await goal.record({ kind: 'otp.failed', subject, ip: '203.0.113.7', userAgent: 'x'.repeat(600) });
    await goal.record({ kind: 'otp.failed', subject, ip: 'not-an-ip' });
    await goal.record({ kind: 'otp.sent', subject });

    expect(await goal.count('otp.failed', subject, before)).toBe(2);
    expect(await goal.count('otp.failed', subject, new Date(Date.now() + 60_000))).toBe(0);
    expect(await goal.count('otp.failed', emailSubject('other@example.com'), before)).toBe(0);

    const { rows } = await admin.query(
      `SELECT host(ip) AS ip, length(user_agent) AS ua FROM auth_events WHERE subject = $1 AND kind = 'otp.failed' ORDER BY id`,
      [subject],
    );
    expect(rows).toEqual([
      { ip: '203.0.113.7', ua: 512 },
      { ip: null, ua: null },
    ]);
  });

  it('refuse a kind that goald writes, so the auth service cannot forge it', async () => {
    await expect(
      auth.query(`SELECT goal_auth_event('platform_admin.added', NULL, NULL, NULL, NULL, '{}')`),
    ).rejects.toThrow(/does not write platform_admin.added/);
  });

  it('are the only way in: goal_auth cannot touch auth_events itself', async () => {
    await expect(auth.query(`SELECT count(*) FROM auth_events`)).rejects.toThrow(/permission denied/);
  });
});

describe('goal_org_factories', () => {
  it('lists a live organization’s factories by name, and none of a deleted one', async () => {
    const org = await fx.org('factories-org');
    const b = await fx.factory(org, 'Beta line');
    const a = await fx.factory(org, 'Alpha line');
    const gone = await fx.org('factories-gone');
    await fx.factory(gone, 'Gone line');
    await admin.query(`UPDATE organizations SET deleted_at = now() WHERE id = $1`, [gone]);

    expect(await goal.orgFactories(org)).toEqual([
      { id: a, name: 'Alpha line' },
      { id: b, name: 'Beta line' },
    ]);
    expect(await goal.orgFactories(gone)).toEqual([]);
  });
});

describe('goal_apply_invitation_roles', () => {
  let org: string;
  let inviter: string;
  let factory: string;

  beforeEach(async () => {
    org = await fx.org(`roles-${Math.random().toString(36).slice(2, 8)}`);
    inviter = await fx.user(`inviter-${org}@example.com`, 'M. Okafor');
    await fx.member(org, inviter, 'owner');
    factory = await fx.factory(org, 'Main line');
  });

  async function accepted(email: string, factoryRoles: unknown): Promise<string> {
    const invitation = await fx.invitation(org, email, inviter, { factoryRoles: JSON.stringify(factoryRoles) });
    await admin.query(`UPDATE auth.invitation SET status = 'accepted' WHERE id = $1`, [invitation]);
    return invitation;
  }

  async function roles(user: string) {
    const { rows } = await admin.query(
      `SELECT factory_id, role, granted_by FROM factory_roles WHERE org_id = $1 AND user_id = $2`,
      [org, user],
    );
    return rows;
  }

  it('grants the invitee the roles of factories that exist, once, audited as the inviter', async () => {
    const email = `ada-${org}@example.com`;
    const invitee = await fx.user(email, 'Ada');
    await fx.member(org, invitee, 'member');
    const invitation = await accepted(email, [
      { factoryId: factory, role: 'approver' },
      { factoryId: '019a1c00-0000-7000-8000-00000000dead', role: 'editor' },
      { factoryId: 'not-a-factory', role: 'viewer' },
    ]);

    await goal.applyInvitationRoles(invitation);
    await goal.applyInvitationRoles(invitation);

    expect(await roles(invitee)).toEqual([{ factory_id: factory, role: 'approver', granted_by: `user:${inviter}` }]);
    const { rows } = await admin.query(
      `SELECT actor_kind, actor_id, actor_name, action, target_id, factory_id, data
       FROM audit_log WHERE org_id = $1 AND action = 'factory_role.granted'`,
      [org],
    );
    expect(rows).toEqual([
      {
        actor_kind: 'user',
        actor_id: inviter,
        actor_name: 'M. Okafor',
        action: 'factory_role.granted',
        target_id: invitee,
        factory_id: factory,
        data: { user_id: invitee, factory_id: factory, to: 'approver', via: 'invitation' },
      },
    ]);
  });

  it('does nothing for a pending invitation or an invitee who is not a member', async () => {
    const email = `bo-${org}@example.com`;
    const invitee = await fx.user(email);
    const pending = await fx.invitation(org, email, inviter, {
      factoryRoles: JSON.stringify([{ factoryId: factory, role: 'viewer' }]),
    });
    await goal.applyInvitationRoles(pending);

    const notMember = await accepted(email, [{ factoryId: factory, role: 'viewer' }]);
    await goal.applyInvitationRoles(notMember);

    await fx.member(org, invitee, 'member');
    expect(await roles(invitee)).toEqual([]);
  });
});

describe('goal_claim_bootstrap', () => {
  let fresh: TestDatabase;
  let pool: pg.Pool;
  let superuser: pg.Pool;

  beforeAll(async () => {
    fresh = await createTestDatabase();
    pool = new pg.Pool({ connectionString: fresh.url('goal_auth') });
    superuser = new pg.Pool({ connectionString: fresh.url() });
  });

  afterAll(async () => {
    await Promise.all([pool?.end(), superuser?.end()]);
    await fresh?.drop();
  });

  it('makes the earliest user platform admin and owner of every live organization, once', async () => {
    const f = new Fixtures(superuser);
    const live = await f.org('bootstrap-live');
    const gone = await f.org('bootstrap-gone');
    await superuser.query(`UPDATE organizations SET deleted_at = now() WHERE id = $1`, [gone]);
    const first = await f.user('owner@example.com');
    const second = await f.user('second@example.com');
    const g = new GoalDatabase(pool);

    expect(await g.claimBootstrap(second)).toBe(false);
    expect(await Promise.all([g.claimBootstrap(first), g.claimBootstrap(first)])).toEqual([true, true]);
    expect(await g.claimBootstrap(second)).toBe(false);

    const admins = await superuser.query(`SELECT user_id, added_by FROM platform_admins`);
    expect(admins.rows).toEqual([{ user_id: first, added_by: 'system:bootstrap' }]);
    const owners = await superuser.query(
      `SELECT "organizationId" AS org, role, "changedBy" AS by FROM auth.member WHERE "userId" = $1 ORDER BY 1`,
      [first],
    );
    const defaultOrg = '019a1c00-0000-7000-8000-000000000001';
    expect(owners.rows).toEqual(
      expect.arrayContaining([
        { org: defaultOrg, role: 'owner', by: 'system:bootstrap' },
        { org: live, role: 'owner', by: 'system:bootstrap' },
      ]),
    );
    expect(owners.rows.map((r) => r.org)).not.toContain(gone);
    const events = await superuser.query(`SELECT user_id, data FROM auth_events WHERE kind = 'platform_admin.added'`);
    expect(events.rows).toEqual([{ user_id: first, data: { via: 'bootstrap' } }]);
  });
});
